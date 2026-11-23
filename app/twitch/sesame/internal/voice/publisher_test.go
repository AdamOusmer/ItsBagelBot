// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/internal/voice"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type publishCase struct {
	name       string
	allowed    bool
	maxSeconds int
	remaining  int64
	override   string
	settings   string
	wantVoice  string
	wantRunes  int
}

func TestPublisherPublishesResolvedJobs(t *testing.T) {
	cases := []publishCase{
		{name: "denied", maxSeconds: 5, remaining: 500},
		{name: "remaining below one second", allowed: true, maxSeconds: 5, remaining: 24},
		{name: "remaining budget", allowed: true, maxSeconds: 5, remaining: 50, wantVoice: voiceplan.DefaultVoice, wantRunes: 28},
		{name: "clip length and override", allowed: true, maxSeconds: 5, remaining: 500, override: "Zephyr", wantVoice: "Zephyr", wantRunes: 70},
		{name: "settings voice", allowed: true, maxSeconds: 5, remaining: 500, settings: "Kore", wantVoice: "Kore", wantRunes: 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc publishCase) run(t *testing.T) {
	nc := bustest.JetStream(t)
	serveFake(t, nc, usersrpc.VoiceCheckSubject, func(context.Context, usersrpc.VoiceCheckRequest) usersrpc.VoiceCheckReply {
		return usersrpc.VoiceCheckReply{Allowed: tc.allowed, VoiceReply: usersrpc.VoiceReply{VoiceAllowance: usersrpc.VoiceAllowance{RemainingTokens: tc.remaining}}}
	})
	serveFake(t, nc, overlaysrpc.TtsSettingsSubject, func(context.Context, overlaysrpc.TtsUserRequest) overlaysrpc.TtsSettingsReply {
		settings := overlaysrpc.DefaultTtsSettings()
		settings.MaxSeconds = tc.maxSeconds
		settings.Voice = tc.settings
		return overlaysrpc.TtsSettingsReply{Configured: true, Settings: settings}
	})
	jobs, err := bus.OpenGossipJobs(t.Context(), nc, 1)
	require.NoError(t, err)
	job := voiceplan.AlertJob{
		EventID: "52321f6ed12e5e17e61656395631563a", BroadcasterID: "42", Kind: "sub", Text: strings.Repeat("é", 90),
		Style: "warm, friendly, upbeat", Voice: tc.override, CreatedAt: time.Now(),
	}
	require.NoError(t, voice.Publisher{RPC: nc, Jobs: jobs, Log: zap.NewNop()}.Publish(t.Context(), job))

	js, err := jetstream.NewWithDomain(nc, bus.JSDomain())
	require.NoError(t, err)
	stream, err := js.Stream(t.Context(), bus.GossipJobsStream)
	require.NoError(t, err)
	info, err := stream.Info(t.Context())
	require.NoError(t, err)
	if tc.wantRunes == 0 {
		assert.Zero(t, info.State.Msgs)
		return
	}
	require.EqualValues(t, 1, info.State.Msgs)
	msg, err := stream.GetMsg(t.Context(), info.State.LastSeq)
	require.NoError(t, err)
	var got voiceplan.AlertJob
	require.NoError(t, codec.Unmarshal(msg.Data, &got))
	assert.Equal(t, "gossip.jobs.tts.42", msg.Subject)
	assert.Equal(t, strings.Repeat("é", tc.wantRunes), got.Text)
	assert.Equal(t, tc.wantVoice, got.Voice)
}

func serveFake[Req, Resp any](t *testing.T, nc *nats.Conn, subject string, handle func(context.Context, Req) Resp) {
	t.Helper()
	sub := bus.RPCSubscription{Subject: subject, QueueGroup: "fake-voice"}
	require.NoError(t, bus.QueueSubscribeJSONConcurrent(nc, sub, time.Second, nil, zap.NewNop(), handle))
}
