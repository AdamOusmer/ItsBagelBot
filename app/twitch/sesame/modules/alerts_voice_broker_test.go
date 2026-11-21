// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/voice"
	"ItsBagelBot/internal/domain/event/lane"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type alertBrokerCase struct {
	name       string
	chatAlerts bool
	eventID    string
	wantID     string
	traceID    string
}

func TestProcessEventSubVoiceJobsReachJetStream(t *testing.T) {
	const traceID = "5292b5d5721e379066e91a8897b7901a"
	cases := []alertBrokerCase{
		{name: "voice with Chat Alerts disabled", wantID: alertNotificationDigest},
		{name: "voice and chat with Chat Alerts enabled", chatAlerts: true, wantID: alertNotificationDigest},
		{name: "explicit event ID takes precedence", eventID: "explicit-event", wantID: "ffe9df244d41e848b72a82f87e106bff"},
		{name: "originating trace takes precedence", eventID: "explicit-event", wantID: traceID, traceID: traceID},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc alertBrokerCase) run(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core)
	publisher, stream := openAlertTestStream(t, log)
	out := bustest.NewPublisher()
	d := engine.Deps{
		AlertVoice: publisher, Log: log, Pub: out, Live: &fakeLive{live: true},
		Dedup: engine.NewEventDedup(loyaltyClaims{}, "voice:", time.Hour, log),
		Proj:  &fakeProj{modules: []projection.ModuleView{{Name: "alerts", IsEnabled: tc.chatAlerts, Configs: []byte(`{"cheerTts":"true","cheerTtsStyle":"calm"}`)}}},
	}
	p := engine.NewPipeline(d, alertTestRegistry(d), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)

	env := lane.Envelope{
		Type: "channel.cheer", Lane: "standard",
		MsgID: alertNotificationID, EventID: tc.eventID,
		Event: []byte(`{"user_name":"CoolViewer","broadcaster_user_id":"2","bits":100,"message":"Cheer100 Hello!"}`),
	}
	msg := alertEnvelopeMessage(t, env)
	if tc.traceID != "" {
		msg.Metadata.Set("traceparent", tc.traceparent())
	}
	require.NoError(t, p.Process(msg))
	assert.Equal(t, tc.chatAlerts, len(out.On("out")) == 1)
	replay := alertEnvelopeMessage(t, env)
	replay.Metadata.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	require.NoError(t, p.Process(replay))
	tc.assertJob(t, stream)
	entries := logs.FilterMessage("voice job published").All()
	require.Len(t, entries, 1)
	assert.Equal(t, tc.traceID, entries[0].ContextMap()["trace_id"])
}

func (tc alertBrokerCase) traceparent() string {
	if tc.traceID == "" {
		return ""
	}
	return "00-" + tc.traceID + "-00f067aa0ba902b7-01"
}

func openAlertTestStream(t *testing.T, log *zap.Logger) (voice.Publisher, jetstream.Stream) {
	t.Helper()
	ctx := t.Context()
	nc := bustest.JetStream(t)
	sub := bus.RPCSubscription{Subject: usersrpc.VoiceCheckSubject, QueueGroup: "test-voice"}
	require.NoError(t, bus.QueueSubscribeJSONConcurrent(nc, sub, time.Second, nil, log, allowTestVoice))
	jobs, err := bus.OpenTTSJobs(ctx, nc, 1)
	require.NoError(t, err)
	js, err := jetstream.NewWithDomain(nc, bus.JSDomain())
	require.NoError(t, err)
	stream, err := js.Stream(ctx, bus.TTSJobsStream)
	require.NoError(t, err)
	return voice.Publisher{RPC: nc, Jobs: jobs}, stream
}

func allowTestVoice(_ context.Context, req usersrpc.VoiceCheckRequest) usersrpc.VoiceCheckReply {
	return usersrpc.VoiceCheckReply{Allowed: req.UserID == "2"}
}

func (tc alertBrokerCase) assertJob(t *testing.T, stream jetstream.Stream) {
	t.Helper()
	ctx := t.Context()
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, info.State.LastSeq)
	msg, err := stream.GetMsg(ctx, info.State.LastSeq)
	require.NoError(t, err)

	var job voiceplan.AlertJob
	require.NoError(t, codec.Unmarshal(msg.Data, &job))
	assert.Equal(t, []string{tc.wantID, tc.wantID, tc.traceID, tc.traceparent(), "2", "CoolViewer said: Hello!", "calm, gentle, relaxed"},
		[]string{job.EventID, msg.Header.Get(nats.MsgIdHdr), job.TraceID, msg.Header.Get("Traceparent"), job.BroadcasterID, job.Text, job.Style})
}
