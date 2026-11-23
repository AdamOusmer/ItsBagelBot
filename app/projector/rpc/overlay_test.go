// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type overlayFixture struct {
	f      poweredFixture
	owned  map[overlaysrpc.TokenHash]overlaysrpc.Overlay
	owners *atomic.Int32
}

func newOverlayFixture(t *testing.T, owned ...overlaysrpc.Overlay) overlayFixture {
	t.Helper()
	o := overlayFixture{owned: map[overlaysrpc.TokenHash]overlaysrpc.Overlay{}, owners: &atomic.Int32{}}
	for _, overlay := range owned {
		o.owned[overlay.TokenHash] = overlay
	}
	nc := bustest.NATS(t)
	o.f = poweredFixture{nc: nc, store: projection.NewStore(valkeytest.Spawn(t)), hub: bus.NewInvalidations(nil, "", zap.NewNop()), owner: o.owners}
	wiring := bus.RPCWiring{NC: nc, Queue: "overlay-test", Log: zap.NewNop()}
	require.NoError(t, bus.Serve(wiring, overlaysrpc.OwnerPoweredByTokenSubject, o.ownerReply))
	require.NoError(t, SubscribePowered(wiring, PoweredConfig{Store: o.f.store, Invalidations: o.f.hub}))
	return o
}

func (o overlayFixture) ownerReply(_ context.Context, req overlaysrpc.OwnerPoweredByTokenRequest) overlaysrpc.OwnerPoweredReply {
	o.owners.Add(1)
	if overlay, ok := o.owned[req.TokenHash]; ok {
		return overlaysrpc.OwnerPoweredReply{Unified: &overlay}
	}
	return overlaysrpc.OwnerPoweredReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "overlay not found")}
}

func (o overlayFixture) resolve(t *testing.T, hash overlaysrpc.TokenHash) overlaysrpc.OverlayResolveReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var reply overlaysrpc.OverlayResolveReply
	request := bus.RPCRequest{Subject: overlaysrpc.OverlayResolveSubject, Body: overlaysrpc.OverlayResolveRequest{TokenHash: hash}, DecodeErrorReply: true}
	require.NoError(t, bus.RequestInto(ctx, o.f.nc, request, &reply))
	return reply
}

func testOverlay(id, userID uint64, kind, token string) overlaysrpc.Overlay {
	return overlaysrpc.Overlay{
		ID: id, UserID: userID, Kind: kind, Template: "powered", Name: "Powered by Bagel",
		TokenHash: overlaysrpc.HashToken(token), Generation: 3, Revision: 1, Document: []byte(`{"version":2}`),
	}
}

func TestOverlayResolveAllowsByKindAndAlpha(t *testing.T) {
	system := testOverlay(1, 41, "system", "system")
	customFree := testOverlay(2, 42, "custom", "custom-free")
	customPaid := testOverlay(3, 43, "custom", "custom-paid")
	customAlpha := testOverlay(6, 46, "custom", "custom-alpha")
	o := newOverlayFixture(t, system, customFree, customPaid, customAlpha)
	ctx := t.Context()
	require.NoError(t, o.f.store.SetUser(ctx, 43, projection.UserProjection{Status: "premium", IsActive: true}))
	require.NoError(t, o.f.store.SetUser(ctx, 46, projection.UserProjection{Status: "free", IsActive: true, Alpha: true}))

	cases := []struct {
		name    string
		overlay overlaysrpc.Overlay
		allowed bool
		premium bool
	}{
		{"system overlay for a free user", system, true, false},
		{"custom overlay for a free user", customFree, false, false},
		{"custom overlay for a premium user without alpha", customPaid, false, true},
		{"custom overlay for an alpha user", customAlpha, true, false},
	}
	for _, c := range cases {
		reply := o.resolve(t, c.overlay.TokenHash)
		want := overlaysrpc.OverlayResolveReply{
			UserID: c.overlay.UserID, OverlayID: c.overlay.ID, Kind: c.overlay.Kind, Template: "powered",
			Revision: 1, Generation: 3, Allowed: c.allowed, Premium: c.premium,
		}
		if c.allowed {
			want.Document = c.overlay.Document
		}
		assert.Equal(t, want, reply, c.name)
	}
}

func TestOverlayResolveStopsWhenAlphaIsRevoked(t *testing.T) {
	custom := testOverlay(7, 47, "custom", "custom-revoked")
	o := newOverlayFixture(t, custom)
	ctx := t.Context()
	require.NoError(t, o.f.store.SetUser(ctx, 47, projection.UserProjection{Status: "premium", IsActive: true, Alpha: true}))
	granted := o.resolve(t, custom.TokenHash)

	require.NoError(t, o.f.store.SetUser(ctx, 47, projection.UserProjection{Status: "premium", IsActive: true}))
	require.NoError(t, o.f.hub.Publish(invalidate.ScopeUserProjection, 47))
	revoked := o.resolve(t, custom.TokenHash)

	assert.True(t, granted.Allowed)
	assert.NotEmpty(t, granted.Document)
	assert.False(t, revoked.Allowed)
	assert.Empty(t, revoked.Document)
}

func TestOverlayResolveReadsProjectionThenOwner(t *testing.T) {
	warm := testOverlay(4, 44, "system", "warm")
	cold := testOverlay(5, 45, "system", "cold")
	o := newOverlayFixture(t, cold)
	_, err := o.f.store.FoldOverlay(t.Context(), data.OverlayChangedDTO{Overlay: warm})
	require.NoError(t, err)

	cases := []struct {
		name       string
		hash       overlaysrpc.TokenHash
		code       domainrpc.Code
		ownerCalls int32
	}{
		{"projected overlay", warm.TokenHash, domainrpc.CodeOK, 0},
		{"owner fills a cold overlay", cold.TokenHash, domainrpc.CodeOK, 1},
		{"cold overlay served from cache", cold.TokenHash, domainrpc.CodeOK, 1},
		{"unknown overlay", overlaysrpc.HashToken("missing"), domainrpc.CodeNotFound, 2},
		{"malformed hash", "abc", domainrpc.CodeNotFound, 2},
	}
	for _, c := range cases {
		assert.Equal(t, c.code, o.resolve(t, c.hash).Code, c.name)
		assert.Equal(t, c.ownerCalls, o.owners.Load(), c.name)
	}
}
