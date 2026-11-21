// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	sesamerpc "ItsBagelBot/internal/domain/rpc/sesame"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type voiceTestJobs chan voiceplan.AlertJob

func (voiceTestJobs) Enabled(context.Context, string) (bool, error) { return true, nil }

func (jobs voiceTestJobs) Publish(_ context.Context, job voiceplan.AlertJob) error {
	jobs <- job
	return nil
}

type voiceTestFixture struct {
	allowance usersrpc.VoiceCheckReply
	settings  overlaysrpc.TtsSettingsReply
	config    []byte
	limit     voiceTestLimit
}

func (f voiceTestFixture) serve(t *testing.T) (*nats.Conn, voiceTestJobs) {
	t.Helper()
	nc := bustest.NATS(t)
	log := zap.NewNop()
	wiring := bus.RPCWiring{NC: nc, Queue: "test-voice", Log: log}
	require.NoError(t, bus.Serve(wiring, usersrpc.VoiceCheckSubject, f.check))
	require.NoError(t, bus.Serve(wiring, overlaysrpc.TtsSettingsSubject, f.ttsSettings))
	jobs := make(voiceTestJobs, 3)
	d := engine.Deps{
		AlertVoice: jobs, Log: log, Live: &fakeLive{live: false},
		Proj: &fakeProj{
			user:    projection.User{Locale: "fr"},
			modules: []projection.ModuleView{{Name: "alerts", Configs: f.config}},
		},
	}
	require.NoError(t, SubscribeVoiceTestRPC(wiring, d, f.limit))
	return nc, jobs
}

func (f voiceTestFixture) check(_ context.Context, req usersrpc.VoiceCheckRequest) usersrpc.VoiceCheckReply {
	if req.UserID != "2" && req.UserID != "3" {
		return usersrpc.VoiceCheckReply{}
	}
	return f.allowance
}

func (f voiceTestFixture) ttsSettings(_ context.Context, req overlaysrpc.TtsUserRequest) overlaysrpc.TtsSettingsReply {
	if req.UserID != "2" && req.UserID != "3" {
		return overlaysrpc.TtsSettingsReply{}
	}
	return f.settings
}

func requestVoiceTest(t *testing.T, nc *nats.Conn, req sesamerpc.VoiceTestRequest) sesamerpc.VoiceTestReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var reply sesamerpc.VoiceTestReply
	err := bus.RequestInto(ctx, nc, bus.RPCRequest{Subject: sesamerpc.VoiceTestSubject, Body: req, DecodeErrorReply: true}, &reply)
	require.NoError(t, err)
	return reply
}

func allowVoiceTest(context.Context, ratelimit.Request) (bool, error) { return true, nil }

func voiceTestAllowance() usersrpc.VoiceCheckReply {
	return usersrpc.VoiceCheckReply{Allowed: true, VoiceReply: usersrpc.VoiceReply{
		VoiceAllowance: usersrpc.VoiceAllowance{Reason: "ok", RemainingTokens: 3000},
	}}
}

func voiceTestSettings() overlaysrpc.TtsSettingsReply {
	return overlaysrpc.TtsSettingsReply{Configured: true, Settings: overlaysrpc.TtsSettings{Enabled: true, Voice: "Puck", MaxSeconds: 30}}
}

func voiceTestOwnerRequest() sesamerpc.VoiceTestRequest {
	return sesamerpc.VoiceTestRequest{BroadcasterUserID: "2", ActorID: "2", DisplayName: "Streamer"}
}

func TestVoiceTestAlertRPCQueuesLocalizedCheerWhileOffline(t *testing.T) {
	fixture := voiceTestFixture{allowance: voiceTestAllowance(), settings: voiceTestSettings(), limit: allowVoiceTest,
		config: []byte(`{"ttsLiveOnly":"true","cheerTts":"false","cheerTtsVoice":"Kore","cheerTtsStyle":"calm"}`)}
	nc, jobs := fixture.serve(t)

	ids := make([]string, 0, 2)
	for range 2 {
		reply := requestVoiceTest(t, nc, voiceTestOwnerRequest())
		require.Equal(t, sesamerpc.VoiceTestQueued, reply.Status, reply.Error)
		require.Len(t, jobs, 1)
		job := <-jobs
		assert.Regexp(t, `^[0-9a-f]{32}$`, job.EventID)
		assert.Equal(t, []string{"2", "cheer", "Streamer a dit : Ceci est un test de votre voix Premium.", "Kore", "calm, gentle, relaxed"},
			[]string{job.BroadcasterID, string(job.Kind), job.Text, job.Voice, job.Style})
		ids = append(ids, job.EventID)
	}
	assert.NotEqual(t, ids[0], ids[1])
}

func TestVoiceTestAlertRPCRefusesWithoutQueueing(t *testing.T) {
	cases := []struct {
		name     string
		actor    string
		overlay  bool
		tokens   int64
		enabled  bool
		expected sesamerpc.VoiceTestReply
	}{
		{"no OBS link", "2", false, 3000, true, sesamerpc.VoiceTestReply{Status: sesamerpc.VoiceTestNoLink}},
		{"exhausted", "2", true, 0, true, sesamerpc.VoiceTestReply{Status: sesamerpc.VoiceTestNoMinutes}},
		{"less than a second", "2", true, 24, true, sesamerpc.VoiceTestReply{Status: sesamerpc.VoiceTestNoMinutes}},
		{"voice off", "2", true, 3000, false, sesamerpc.VoiceTestReply{Status: sesamerpc.VoiceTestDisabled}},
		{"different actor", "3", true, 3000, true, sesamerpc.VoiceTestReply{Refusal: rpc.Refused(rpc.CodeForbidden, "forbidden")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowance, settings := voiceTestAllowance(), voiceTestSettings()
			allowance.RemainingTokens, allowance.Allowed = tc.tokens, tc.tokens > 0
			settings.Configured, settings.Settings.Enabled = tc.overlay, tc.enabled
			fixture := voiceTestFixture{allowance: allowance, settings: settings, limit: allowVoiceTest}
			nc, jobs := fixture.serve(t)
			req := voiceTestOwnerRequest()
			req.ActorID = tc.actor
			assert.Equal(t, tc.expected, requestVoiceTest(t, nc, req))
			assert.Empty(t, jobs)
		})
	}
}

func TestVoiceTestAlertRPCRateLimitsEachChannel(t *testing.T) {
	fixture := voiceTestFixture{allowance: voiceTestAllowance(), settings: voiceTestSettings(), limit: ratelimit.New(valkeytest.Spawn(t)).Allow}
	nc, jobs := fixture.serve(t)
	owner := voiceTestOwnerRequest()
	other := sesamerpc.VoiceTestRequest{BroadcasterUserID: "3", ActorID: "3", DisplayName: "Other"}
	statuses := []sesamerpc.VoiceTestStatus{
		requestVoiceTest(t, nc, owner).Status,
		requestVoiceTest(t, nc, owner).Status,
		requestVoiceTest(t, nc, other).Status,
	}
	assert.Equal(t, []sesamerpc.VoiceTestStatus{sesamerpc.VoiceTestQueued, sesamerpc.VoiceTestRateLimited, sesamerpc.VoiceTestQueued}, statuses)
	require.Len(t, jobs, 2)
	first, second := <-jobs, <-jobs
	assert.Equal(t, []string{"2", "Puck", "3", "Puck"}, []string{first.BroadcasterID, first.Voice, second.BroadcasterID, second.Voice})
}
