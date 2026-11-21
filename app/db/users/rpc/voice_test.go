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
				VoiceGetRequest: req, Enabled: false, Voice: "Leda", MaxSeconds: voiceplan.MaxClipSeconds,
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

func TestDashboardVoiceMaxSeconds(t *testing.T) {
	h := newVoiceHarness(t)
	owner := usersrpc.VoiceGetRequest{ActorID: testVoiceOwner, BroadcasterUserID: testVoiceOwner}
	subject := testVoiceDashboard + ".voice_set"
	omitted := call[voiceSettingsReply](t, h, subject, owner)
	assert.Equal(t, domainrpc.CodeInvalid, omitted.Code)

	cases := []struct {
		name    string
		seconds int
		code    domainrpc.Code
		stored  int
	}{
		{"zero", 0, domainrpc.CodeInvalid, 120},
		{"below minimum", 4, domainrpc.CodeInvalid, 120},
		{"at minimum", 5, domainrpc.CodeOK, 5},
		{"above maximum", 121, domainrpc.CodeInvalid, 5},
		{"at maximum", 120, domainrpc.CodeOK, 120},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := call[voiceSettingsReply](t, h, subject, usersrpc.VoiceSetRequest{
				VoiceGetRequest: owner, Enabled: true, Voice: voiceplan.DefaultVoice, MaxSeconds: tc.seconds,
			})
			assert.Equal(t, tc.code, set.Code)

			settings := call[usersrpc.VoiceReply](t, h, testVoiceDashboard+".voice_get", owner)
			check := call[usersrpc.VoiceCheckReply](t, h, testVoiceCheckSubject, usersrpc.VoiceCheckRequest{UserID: testVoiceOwner})
			assert.Equal(t, [2]int{tc.stored, tc.stored}, [2]int{settings.MaxSeconds, check.MaxSeconds})
		})
	}
}

type voiceSettingsReply struct {
	OK bool `json:"ok"`
	domainrpc.Refusal
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
			got := call[usersrpc.VoiceCheckReply](t, h, testVoiceCheckSubject, usersrpc.VoiceCheckRequest{UserID: testVoiceOwner})
			assert.Empty(t, got.Refusal)
			assert.Equal(t, [2]bool{tc.allowed, tc.enabled}, [2]bool{got.Allowed, got.Enabled})
			assert.Equal(t, [2]string{voiceplan.DefaultVoice, tc.reason}, [2]string{got.Voice, got.Reason})
		})
	}
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
