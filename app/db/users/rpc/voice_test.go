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

func TestInternalVoiceCheck(t *testing.T) {
	h := newVoiceHarness(t)
	cases := []struct {
		name            string
		status          user.Status
		active, enabled bool
		allowed         bool
	}{
		{"vip enabled", user.StatusVip, true, true, true},
		{"vip disabled", user.StatusVip, true, false, false},
		{"inactive", user.StatusVip, false, true, false},
		{"free", user.StatusFree, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h.client.User.UpdateOneID(9000).SetStatus(tc.status).SetIsActive(tc.active).SetTtsEnabled(tc.enabled).ExecX(t.Context())
			got := call[usersrpc.VoiceCheckReply](t, h, testVoiceCheckSubject, usersrpc.VoiceCheckRequest{UserID: testVoiceOwner})
			assert.Empty(t, got.Refusal)
			assert.Equal(t, [2]bool{tc.allowed, tc.enabled}, [2]bool{got.Allowed, got.Enabled})
			assert.Equal(t, voiceplan.DefaultVoice, got.Voice)
		})
	}
}

func TestInternalVoiceRecord(t *testing.T) {
	h := newVoiceHarness(t)
	limit := voiceplan.SecondsToTokens(voiceplan.MonthlySeconds(voiceplan.VIP))
	subject := testVoiceRecordSubject
	req := usersrpc.VoiceRecordRequest{UserID: testVoiceOwner, RequestID: "first", AudioTokens: limit + 25, DurationMS: 1000}
	first := call[usersrpc.VoiceReply](t, h, subject, req)
	require.Empty(t, first.Refusal)
	assert.Equal(t, [2]int64{req.AudioTokens, 0}, [2]int64{first.UsedTokens, first.RemainingTokens})
	assert.Equal(t, first, call[usersrpc.VoiceReply](t, h, subject, req))

	req.RequestID, req.DurationMS = "second", -1
	invalid := call[usersrpc.VoiceReply](t, h, subject, req)
	assert.Equal(t, domainrpc.CodeInvalid, invalid.Code)
}
