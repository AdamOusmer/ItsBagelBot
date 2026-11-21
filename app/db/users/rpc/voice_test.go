// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/rpc"
	domainrpc "ItsBagelBot/internal/domain/rpc"
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
	return h
}

func TestDashboardVoiceOwnerOnly(t *testing.T) {
	h := newVoiceHarness(t)
	cases := []struct {
		name, actor string
		code        domainrpc.Code
		voice       string
		enabled     bool
	}{
		{"missing actor", "", domainrpc.CodeForbidden, voiceplan.DefaultVoice, true},
		{"delegate", "9001", domainrpc.CodeForbidden, voiceplan.DefaultVoice, true},
		{"owner", testVoiceOwner, domainrpc.CodeOK, "Leda", false},
	}
	owner := usersrpc.VoiceGetRequest{ActorID: testVoiceOwner, BroadcasterUserID: testVoiceOwner}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := owner
			req.ActorID = tc.actor
			set := call[voiceSettingsReply](t, h, testVoiceDashboard+".voice_set", usersrpc.VoiceSetRequest{
				VoiceGetRequest: req, Enabled: false, Voice: "Leda",
			})
			assert.Equal(t, tc.code, set.Code)
			assert.Equal(t, tc.code == domainrpc.CodeOK, set.OK)

			read := call[usersrpc.VoiceReply](t, h, testVoiceDashboard+".voice_get", req)
			assert.Equal(t, tc.code, read.Code)

			settings := call[usersrpc.VoiceReply](t, h, testVoiceDashboard+".voice_get", owner)
			assert.Equal(t, tc.voice, settings.Voice)
			assert.Equal(t, tc.enabled, settings.Enabled)
		})
	}
}

type voiceSettingsReply struct {
	OK bool `json:"ok"`
	domainrpc.Refusal
}

type voiceCheckResult struct {
	usersrpc.VoiceCheckReply
	Reason string `json:"reason"`
}

func TestInternalVoiceCheck(t *testing.T) {
	h := newVoiceHarness(t)
	cases := []struct {
		name                    string
		status                  user.Status
		active, enabled, banned bool
		allowed                 bool
		reason                  string
	}{
		{"vip enabled", user.StatusVip, true, true, false, true, "ok"},
		{"vip disabled", user.StatusVip, true, false, false, false, "ok"},
		{"inactive", user.StatusVip, false, true, false, false, "inactive"},
		{"free", user.StatusFree, true, true, false, false, "no_plan"},
		{"banned", user.StatusVip, true, true, true, false, "banned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.client.User.UpdateOneID(9000).SetStatus(tc.status).SetIsActive(tc.active).
				SetTtsEnabled(tc.enabled).SetBanned(tc.banned).ExecX(t.Context())
			got := call[voiceCheckResult](t, h, testVoiceCheckSubject, usersrpc.VoiceCheckRequest{UserID: testVoiceOwner})
			assert.Empty(t, got.Refusal)
			assert.Equal(t, [2]bool{tc.allowed, tc.enabled}, [2]bool{got.Allowed, got.Enabled})
			assert.Equal(t, [2]string{voiceplan.DefaultVoice, tc.reason}, [2]string{got.Voice, got.Reason})
		})
	}
}

func TestDashboardVoiceInactiveReason(t *testing.T) {
	h := newVoiceHarness(t)
	h.client.User.UpdateOneID(9000).SetIsActive(false).ExecX(t.Context())
	type result struct {
		usersrpc.VoiceReply
		Reason string `json:"reason"`
	}
	req := usersrpc.VoiceGetRequest{ActorID: testVoiceOwner, BroadcasterUserID: testVoiceOwner}
	reply := call[result](t, h, testVoiceDashboard+".voice_get", req)
	assert.Empty(t, reply.Refusal)
	assert.Equal(t, voiceplan.Free, reply.Plan)
	assert.Equal(t, "inactive", reply.Reason)
}

func TestInternalVoiceRecord(t *testing.T) {
	h := newVoiceHarness(t)
	limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(voiceplan.VIP))
	subject := testVoiceRecordSubject
	req := usersrpc.VoiceRecordRequest{UserID: testVoiceOwner, RequestID: "first", AudioTokens: 15_000, DurationMS: 600_000}
	first := call[usersrpc.VoiceReply](t, h, subject, req)
	require.Empty(t, first.Refusal)
	assert.Equal(t, [2]int64{req.AudioTokens, limit - req.AudioTokens}, [2]int64{first.UsedTokens, first.RemainingTokens})
	assert.Equal(t, first, call[usersrpc.VoiceReply](t, h, subject, req))

	for _, usage := range []usersrpc.VoiceRecordRequest{
		{UserID: testVoiceOwner, RequestID: "negative-duration", DurationMS: -1},
		{UserID: testVoiceOwner, RequestID: "excess-tokens", AudioTokens: 15_001},
		{UserID: testVoiceOwner, RequestID: "excess-duration", DurationMS: 600_001},
	} {
		invalid := call[usersrpc.VoiceReply](t, h, subject, usage)
		assert.Equal(t, domainrpc.CodeInvalid, invalid.Code, "request %s", usage.RequestID)
	}
}
