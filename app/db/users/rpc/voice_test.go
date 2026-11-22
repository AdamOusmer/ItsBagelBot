// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/rpc"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const testVoiceDashboard = "rpc.dashboard"
const testVoiceOwner = "9000"

const (
	testVoiceCheckSubject  = "rpc.internal.users.voice.check"
	testVoiceRecordSubject = "rpc.internal.users.voice.record"
)

func newVoiceHarness(t *testing.T) harness {
	t.Helper()
	h := newHarness(t)
	h.users(1, time.Now())
	h.client.User.UpdateOneID(9000).SetStatus(user.StatusVip).ExecX(t.Context())
	w := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}, Repo: h.repo}
	require.NoError(t, rpc.SubscribeDashboard(w, testVoiceDashboard))
	require.NoError(t, rpc.SubscribeVoice(w, testVoiceCheckSubject, testVoiceRecordSubject))
	require.NoError(t, rpc.SubscribeVoiceExport(w))
	return h
}

func TestDashboardVoiceAllowanceOwnerOnly(t *testing.T) {
	h := newVoiceHarness(t)
	for _, actor := range []string{"", "9001", testVoiceOwner} {
		req := usersrpc.VoiceGetRequest{ActorID: actor, BroadcasterUserID: testVoiceOwner}
		read := call[usersrpc.VoiceReply](t, h, testVoiceDashboard+".voice_get", req)
		assert.Equal(t, actor == testVoiceOwner, read.Code == domainrpc.CodeOK)
	}
}

func TestInternalVoiceCheck(t *testing.T) {
	h := newVoiceHarness(t)
	cases := []struct {
		status                  user.Status
		active, banned, allowed bool
		reason                  string
	}{
		{user.StatusVip, true, false, true, "ok"},
		{user.StatusVip, false, false, false, "inactive"},
		{user.StatusFree, true, false, false, "no_plan"},
		{user.StatusVip, true, true, false, "banned"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			h.client.User.UpdateOneID(9000).SetStatus(tc.status).SetIsActive(tc.active).SetBanned(tc.banned).ExecX(t.Context())
			got := call[usersrpc.VoiceCheckReply](t, h, testVoiceCheckSubject, usersrpc.VoiceCheckRequest{UserID: testVoiceOwner})
			assert.Empty(t, got.Refusal)
			assert.Equal(t, tc.allowed, got.Allowed)
			assert.Equal(t, tc.reason, got.Reason)
		})
	}
}

func TestLegacyVoiceExportSkipsDefaultAccountsAndPagesByID(t *testing.T) {
	h := newVoiceHarness(t)
	now := time.Now().UTC()
	for id := uint64(9001); id <= 9251; id++ {
		h.client.User.Create().SetID(id).SetUsername(fmt.Sprint(id)).SetEmail(fmt.Sprintf("%d@example.invalid", id)).SetCreatedAt(now).
			SetTtsVoice("Puck").SetTtsMaxSeconds(30).SetTtsEnabled(false).ExecX(t.Context())
	}
	hash := overlaysrpc.TtsTokenHash(strings.Repeat("a", 64))
	h.client.User.UpdateOneID(9251).SetTtsOverlayTokenHash(string(hash)).SetTtsOverlayGeneration(42).ExecX(t.Context())
	first := call[usersrpc.VoiceExportReply](t, h, usersrpc.VoiceExportSubject, usersrpc.VoiceExportRequest{})
	require.Len(t, first.Overlays, 250)
	assert.Equal(t, []uint64{9001, 9250}, []uint64{first.Overlays[0].UserID, first.NextID})
	last := call[usersrpc.VoiceExportReply](t, h, usersrpc.VoiceExportSubject, usersrpc.VoiceExportRequest{AfterID: first.NextID})
	require.Len(t, last.Overlays, 1)
	assert.Equal(t, overlaysrpc.TtsOverlay{
		UserID: 9251, CreatedAt: now.UnixMicro(), Generation: 42, TokenHash: hash,
		Settings: overlaysrpc.TtsSettings{Voice: "Puck", MaxSeconds: 30},
	}, last.Overlays[0])
	assert.Zero(t, last.NextID)
}

func TestDashboardVoiceInactiveReason(t *testing.T) {
	h := newVoiceHarness(t)
	h.client.User.UpdateOneID(9000).SetIsActive(false).ExecX(t.Context())
	req := usersrpc.VoiceGetRequest{ActorID: testVoiceOwner, BroadcasterUserID: testVoiceOwner}
	reply := call[usersrpc.VoiceReply](t, h, testVoiceDashboard+".voice_get", req)
	assert.Empty(t, reply.Refusal)
	assert.Equal(t, voiceplan.Free, reply.Plan)
	assert.Equal(t, "inactive", reply.Reason)
}

func TestInternalVoiceRecord(t *testing.T) {
	h := newVoiceHarness(t)
	const capTokens = 3_000
	limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(voiceplan.VIP))
	cases := []struct {
		name            string
		tokens          int64
		durationMS      int64
		code            domainrpc.Code
		used, remaining int64
	}{
		{"at cap", capTokens, 120_000, domainrpc.CodeOK, capTokens, limit - capTokens},
		{"excess tokens", 3_001, 120_000, domainrpc.CodeInvalid, 0, 0},
		{"excess duration", capTokens, 120_001, domainrpc.CodeInvalid, 0, 0},
		{"negative duration", 0, -1, domainrpc.CodeInvalid, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := usersrpc.VoiceRecordRequest{
				UserID: testVoiceOwner, RequestID: tc.name, AudioTokens: tc.tokens, DurationMS: tc.durationMS,
			}
			got := call[usersrpc.VoiceReply](t, h, testVoiceRecordSubject, req)
			assert.Equal(t, tc.code, got.Code)
			assert.Equal(t, [2]int64{tc.used, tc.remaining}, [2]int64{got.UsedTokens, got.RemainingTokens})
			assert.Equal(t, got, call[usersrpc.VoiceReply](t, h, testVoiceRecordSubject, req))
		})
	}
}
