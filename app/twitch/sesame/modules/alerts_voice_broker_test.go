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
)

type alertBrokerCase struct {
	name       string
	chatAlerts bool
	eventID    string
	wantID     string
}

func TestProcessEventSubVoiceJobsReachJetStream(t *testing.T) {
	cases := []alertBrokerCase{
		{name: "voice with Chat Alerts disabled", wantID: alertNotificationDigest},
		{name: "voice and chat with Chat Alerts enabled", chatAlerts: true, wantID: alertNotificationDigest},
		{name: "explicit event ID takes precedence", eventID: "explicit-event", wantID: "ffe9df244d41e848b72a82f87e106bff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc alertBrokerCase) run(t *testing.T) {
	log := zap.NewNop()
	publisher, stream := openAlertTestStream(t, log)
	out := bustest.NewPublisher()
	d := engine.Deps{
		AlertVoice: publisher, Log: log, Pub: out, Live: &fakeLive{live: true},
		Proj: &fakeProj{modules: []projection.ModuleView{{Name: "alerts", IsEnabled: tc.chatAlerts, Configs: []byte(`{"cheerTts":"true","cheerTtsStyle":"calm"}`)}}},
	}
	p := engine.NewPipeline(d, alertTestRegistry(d), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)

	env := lane.Envelope{
		Type: "channel.cheer", Lane: "standard",
		MsgID: alertNotificationID, EventID: tc.eventID,
		Event: []byte(`{"user_name":"CoolViewer","broadcaster_user_id":"2","bits":100,"message":"Cheer100 Hello!"}`),
	}
	processAlertEnvelope(t, p, env)
	assert.Equal(t, tc.chatAlerts, len(out.On("out")) == 1)
	processAlertEnvelope(t, p, env)
	tc.assertJob(t, stream)
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
	assert.Equal(t, []string{tc.wantID, tc.wantID, "2", "CoolViewer said: Hello!", "calm, gentle, relaxed"},
		[]string{job.EventID, msg.Header.Get(nats.MsgIdHdr), job.BroadcasterID, job.Text, job.Style})
}
