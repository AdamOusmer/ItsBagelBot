// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/event/data"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	projectionrpc "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
)

const ttsTestUsersSubject = "rpc.internal.projection.users.get"

type ttsTestFixture struct {
	nc           *nats.Conn
	store        *projection.Store
	ownerReads   atomic.Int32
	userReads    atomic.Int32
	legacy       overlaysrpc.TtsOverlay
	registration bus.RPCWiring
}

func newTtsTestFixture(t *testing.T) *ttsTestFixture {
	t.Helper()
	f := &ttsTestFixture{
		nc:    bustest.NATS(t),
		store: projection.NewStore(valkeytest.Spawn(t)),
		legacy: overlaysrpc.TtsOverlay{UserID: 30, CreatedAt: 50, Revision: 1, Generation: 99,
			TokenHash: overlaysrpc.TtsTokenHash(strings.Repeat("a", 64)), Settings: overlaysrpc.DefaultTtsSettings()},
	}
	f.registration = bus.RPCWiring{NC: f.nc, Queue: "tts-test", Log: zap.NewNop()}
	require.NoError(t, SubscribeTts(f.registration, f.store, ttsTestUsersSubject))
	require.NoError(t, bus.Serve(f.registration, overlaysrpc.OwnerTtsByHashSubject, f.owner))
	require.NoError(t, bus.Serve(f.registration, ttsTestUsersSubject, f.account))
	return f
}

func (f *ttsTestFixture) owner(_ context.Context, req overlaysrpc.TtsResolveRequest) overlaysrpc.OwnerTtsReply {
	f.ownerReads.Add(1)
	if req.TokenHash != f.legacy.TokenHash {
		return overlaysrpc.OwnerTtsReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "not found")}
	}
	return overlaysrpc.OwnerTtsReply{Overlay: &f.legacy}
}

func (f *ttsTestFixture) account(_ context.Context, _ projectionrpc.Request) projectionrpc.UserReply {
	f.userReads.Add(1)
	return projectionrpc.UserReply{Status: "paid", IsActive: true, VoicePlan: "premium", StateRevision: 1, AccountCreatedAt: 49}
}

func ttsRequest[Reply any](t *testing.T, nc *nats.Conn, subject string, body any) Reply {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var reply Reply
	require.NoError(t, bus.RequestInto(ctx, nc, bus.RPCRequest{Subject: subject, Body: body, DecodeErrorReply: true}, &reply))
	return reply
}

func (f *ttsTestFixture) check(t *testing.T, o overlaysrpc.TtsOverlay) bool {
	t.Helper()
	req := overlaysrpc.TtsCheckRequest{UserID: o.UserID, Generation: o.Generation}
	return ttsRequest[overlaysrpc.TtsCheckReply](t, f.nc, overlaysrpc.TtsCheckSubject, req).Valid
}

func (f *ttsTestFixture) resolve(t *testing.T, o overlaysrpc.TtsOverlay) overlaysrpc.TtsResolveReply {
	t.Helper()
	req := overlaysrpc.TtsResolveRequest{TokenHash: o.TokenHash}
	return ttsRequest[overlaysrpc.TtsResolveReply](t, f.nc, overlaysrpc.TtsResolveSubject, req)
}

func TestTtsResolveWarmsBothProjections(t *testing.T) {
	f := newTtsTestFixture(t)
	want := overlaysrpc.TtsResolveReply{UserID: f.legacy.UserID, Generation: f.legacy.Generation}
	for range 2 {
		assert.Equal(t, want, f.resolve(t, f.legacy))
		assert.True(t, f.check(t, f.legacy))
		reply := ttsRequest[overlaysrpc.TtsSettingsReply](t, f.nc, overlaysrpc.TtsSettingsSubject, overlaysrpc.TtsUserRequest{UserID: "30"})
		assert.Equal(t, overlaysrpc.TtsSettingsReply{Settings: f.legacy.Settings, Configured: true}, reply)
	}
	assert.Equal(t, [2]int32{1, 1}, [2]int32{f.ownerReads.Load(), f.userReads.Load()})
}

func TestTtsSettingsCarryBlockedViewers(t *testing.T) {
	f := newTtsTestFixture(t)
	f.legacy.BlockedViewers = []string{"7", "9"}
	f.resolve(t, f.legacy)
	reply := ttsRequest[overlaysrpc.TtsSettingsReply](t, f.nc, overlaysrpc.TtsSettingsSubject, overlaysrpc.TtsUserRequest{UserID: "30"})
	assert.Equal(t, overlaysrpc.TtsSettingsReply{Settings: f.legacy.Settings, Configured: true, BlockedViewers: []string{"7", "9"}}, reply)
}

func TestTtsResolveAndCheckRejectIneligibleLinks(t *testing.T) {
	f := newTtsTestFixture(t)
	rotated := f.legacy
	rotated.Generation, rotated.Revision, rotated.TokenHash = 100, 2, overlaysrpc.TtsTokenHash(strings.Repeat("b", 64))
	_, err := f.store.FoldTtsOverlay(t.Context(), data.TtsOverlayChangedDTO{Overlay: rotated})
	require.NoError(t, err)
	for i, tc := range []struct {
		name           string
		o              overlaysrpc.TtsOverlay
		active, banned bool
		plan           string
		code           domainrpc.Code
	}{
		{"current", rotated, true, false, "premium", domainrpc.CodeOK},
		{"replaced", f.legacy, true, false, "premium", domainrpc.CodeNotFound},
		{"banned", rotated, true, true, "premium", domainrpc.CodeNotFound},
		{"inactive", rotated, false, false, "premium", domainrpc.CodeNotFound},
		{"no voice plan", rotated, true, false, "free", domainrpc.CodeNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := projection.UserProjection{Status: "paid", StateRevision: int64(i + 1), AccountCreatedAt: 49,
				IsActive: tc.active, Banned: tc.banned, VoicePlan: tc.plan}
			require.NoError(t, f.store.SetUser(t.Context(), rotated.UserID, u))
			assert.Equal(t, tc.code, f.resolve(t, tc.o).Code)
			assert.Equal(t, tc.code == domainrpc.CodeOK, f.check(t, tc.o))
		})
	}
}
