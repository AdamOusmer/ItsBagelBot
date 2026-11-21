// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/rpc"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const overlayRotateSubject = testVoiceDashboard + ".voice_overlay_rotate"
const overlayRevocationSubject = "controls.tts.revoked.9000"

var overlayOwner = usersrpc.VoiceGetRequest{ActorID: testVoiceOwner, BroadcasterUserID: testVoiceOwner}

func TestDashboardOverlayRotationEligibility(t *testing.T) {
	h := newVoiceHarness(t)
	cases := []struct {
		name, actor string
		status      user.Status
		active      bool
		banned      bool
	}{
		{"missing actor", "", user.StatusVip, true, false},
		{"delegate", "9001", user.StatusVip, true, false},
		{"free owner", testVoiceOwner, user.StatusFree, true, false},
		{"inactive owner", testVoiceOwner, user.StatusVip, false, false},
		{"banned owner", testVoiceOwner, user.StatusVip, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.client.User.UpdateOneID(9000).SetStatus(tc.status).SetIsActive(tc.active).SetBanned(tc.banned).ExecX(t.Context())
			req := overlayOwner
			req.ActorID = tc.actor
			reply := call[struct {
				usersrpc.VoiceOverlayRotateReply
				domainrpc.Refusal
			}](t, h, overlayRotateSubject, req)
			assert.Equal(t, domainrpc.CodeForbidden, reply.Code)
			assert.Empty(t, reply.URL)
		})
	}
}

func issueOverlayRPC(t *testing.T, h harness) usersrpc.VoiceOverlayResolveRequest {
	t.Helper()
	reply := call[usersrpc.VoiceOverlayRotateReply](t, h, overlayRotateSubject, overlayOwner)
	token, ok := strings.CutPrefix(reply.URL, "https://overlay.itsbagelbot.com/tts#")
	require.True(t, ok)
	require.Len(t, token, 43)
	sum := sha256.Sum256([]byte(token))
	return usersrpc.VoiceOverlayResolveRequest{TokenHash: hex.EncodeToString(sum[:])}
}

func TestVoiceOverlayRPCContract(t *testing.T) {
	h := newVoiceHarness(t)
	h.repo.SetInvalidationConn(h.nc)
	w := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}, Repo: h.repo}
	require.NoError(t, rpc.SubscribeVoiceOverlay(w))
	revocations, err := h.nc.SubscribeSync(overlayRevocationSubject)
	require.NoError(t, err)

	first := issueOverlayRPC(t, h)
	resolved := call[usersrpc.VoiceOverlayResolveReply](t, h, "rpc.internal.users.voice.overlay.resolve", first)
	assert.Equal(t, uint64(9000), resolved.UserID)
	assert.Empty(t, resolved.Code)
	check := usersrpc.VoiceOverlayCheckRequest{UserID: resolved.UserID, Generation: resolved.Generation}
	assert.True(t, call[usersrpc.VoiceOverlayCheckReply](t, h, "rpc.internal.users.voice.overlay.check", check).Valid)
	issueOverlayRPC(t, h)

	for range 2 {
		msg, err := revocations.NextMsg(time.Second)
		require.NoError(t, err)
		assert.Empty(t, msg.Data)
	}
	refusal := call[usersrpc.VoiceOverlayResolveReply](t, h, "rpc.internal.users.voice.overlay.resolve", first)
	assert.Equal(t, domainrpc.CodeNotFound, refusal.Code)
	assert.NotEmpty(t, refusal.Message)
	assert.False(t, call[usersrpc.VoiceOverlayCheckReply](t, h, "rpc.internal.users.voice.overlay.check", check).Valid)
	got := call[usersrpc.VoiceReply](t, h, testVoiceDashboard+".voice_get", overlayOwner)
	assert.True(t, got.OverlayConfigured)
}
