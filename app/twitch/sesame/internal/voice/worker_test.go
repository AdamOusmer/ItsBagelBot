// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/internal/voice"
	"ItsBagelBot/internal/domain/rpc"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const gossipSubject = "rpc.gossip.tts.synthesize"

var fakeWAV = []byte("RIFF\x28\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\xc0\x5d\x00\x00\x80\xbb\x00\x00\x02\x00\x10\x00data\x04\x00\x00\x00\x00\x00\x00\x00")

type voiceRPCs struct {
	allowance usersrpc.VoiceCheckReply
	loseReply bool
	synthesis chan gossiprpc.Request
	records   sync.Map
	checks    atomic.Int32
	attempts  atomic.Int32
	recorded  atomic.Int32
}

func fakeVoiceRPCs(t *testing.T, nc *nats.Conn, allowance usersrpc.VoiceCheckReply, loseReply bool) *voiceRPCs {
	t.Helper()
	fake := &voiceRPCs{allowance: allowance, loseReply: loseReply, synthesis: make(chan gossiprpc.Request, 4)}
	fakeServe(t, nc, usersrpc.VoiceCheckSubject, fake.check)
	fakeServe(t, nc, usersrpc.VoiceRecordSubject, fake.record)
	fakeServe(t, nc, gossipSubject, fake.synthesize)
	return fake
}

func fakeServe[Req, Resp any](t *testing.T, nc *nats.Conn, subject string, handle func(context.Context, Req) Resp) {
	t.Helper()
	err := bus.QueueSubscribeJSONConcurrent(nc, bus.RPCSubscription{Subject: subject, QueueGroup: "fake-voice"}, time.Second, nil, zap.NewNop(), handle)
	require.NoError(t, err)
}

func (f *voiceRPCs) check(_ context.Context, _ usersrpc.VoiceCheckRequest) usersrpc.VoiceCheckReply {
	f.checks.Add(1)
	return f.allowance
}

func (f *voiceRPCs) record(_ context.Context, req usersrpc.VoiceRecordRequest) usersrpc.VoiceReply {
	if _, existed := f.records.LoadOrStore(req.RequestID, req); !existed {
		f.recorded.Add(1)
	}
	first := f.attempts.Add(1) == 1
	if first && f.loseReply {
		return usersrpc.VoiceReply{Refusal: rpc.Refused(rpc.CodeUnavailable, "retry after commit")}
	}
	return usersrpc.VoiceReply{}
}

func (f *voiceRPCs) synthesize(_ context.Context, req gossiprpc.Request) gossiprpc.TTSSynthesizeReply {
	f.synthesis <- req
	return gossiprpc.TTSSynthesizeReply{Audio: fakeWAV, MimeType: "audio/wav", AudioTokens: 1, DurationMS: 1}
}

type testVoiceRig struct {
	nc      *nats.Conn
	jobs    *bus.TTSJobs
	overlay *bus.TTSOverlay
	js      jsapi.JetStream
}

func newTestVoiceRig(t *testing.T) testVoiceRig {
	t.Helper()
	nc := bustest.JetStream(t)
	jobs, err := bus.OpenTTSJobs(t.Context(), nc, 1)
	require.NoError(t, err)
	overlay, err := bus.OpenTTSOverlay(t.Context(), nc, 1)
	require.NoError(t, err)
	js, err := jsapi.New(nc)
	require.NoError(t, err)
	return testVoiceRig{nc: nc, jobs: jobs, overlay: overlay, js: js}
}

func (r testVoiceRig) process(t *testing.T, job voiceplan.AlertJob) {
	t.Helper()
	worker := voice.Worker{RPC: r.nc, Gossip: gossipSubject, Overlay: r.overlay}
	stop, err := r.jobs.Consume(t.Context(), worker.Process, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	pub := voice.Publisher{RPC: r.nc, Jobs: r.jobs}
	require.NoError(t, pub.Publish(t.Context(), job))
	require.NoError(t, pub.Publish(t.Context(), job))

	stream, err := r.js.Stream(t.Context(), bus.TTSJobsStream)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, err := stream.Info(t.Context())
		return err == nil && info.State.Msgs == 0 && info.State.LastSeq == 1
	}, 10*time.Second, 20*time.Millisecond)
}

type workerCase struct {
	name      string
	allowed   bool
	max       int
	remaining int64
	override  string
	wantVoice string
	wantRunes int
	retry     bool
}

func TestWorkerProcessesJetStreamVoiceJobs(t *testing.T) {
	cases := []workerCase{
		{name: "denied", max: 5, remaining: 500},
		{name: "remaining below one second", allowed: true, max: 5, remaining: 24},
		{name: "remaining budget and record retry", allowed: true, max: 5, remaining: 50, wantVoice: "Puck", wantRunes: 28, retry: true},
		{name: "clip length and override", allowed: true, max: 5, remaining: 500, override: "Zephyr", wantVoice: "Zephyr", wantRunes: 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc workerCase) run(t *testing.T) {
	rig := newTestVoiceRig(t)
	fakes := fakeVoiceRPCs(t, rig.nc, usersrpc.VoiceCheckReply{
		Allowed: tc.allowed,
		VoiceReply: usersrpc.VoiceReply{VoiceAllowance: usersrpc.VoiceAllowance{
			Enabled: tc.allowed, MaxSeconds: tc.max, RemainingTokens: tc.remaining, Voice: "Puck",
		}},
	}, tc.retry)
	job := voiceplan.AlertJob{
		EventID: "ev-12", BroadcasterID: "42", Kind: "sub", Text: strings.Repeat("é", 90),
		Style: "warm, friendly, upbeat", Voice: tc.override, CreatedAt: time.Now(),
	}
	rig.process(t, job)
	assert.EqualValues(t, 1, fakes.checks.Load())
	if tc.wantRunes == 0 {
		rig.assertNoClip(t, fakes)
		return
	}

	tc.assertRequests(t, fakes, job)
	rig.assertDelivery(t, job)
}

func (r testVoiceRig) assertNoClip(t *testing.T, f *voiceRPCs) {
	t.Helper()
	clip, err := r.overlay.GetClip(t.Context(), bus.TTSClip{Key: "42/ev-12"})
	require.NoError(t, err)
	assert.Empty(t, clip.Key)
	assert.Empty(t, f.synthesis)
	assert.Zero(t, f.attempts.Load())
}

func (tc workerCase) assertRequests(t *testing.T, f *voiceRPCs, job voiceplan.AlertJob) {
	t.Helper()
	require.Len(t, f.synthesis, 1)
	req := <-f.synthesis
	assert.Equal(t, gossiprpc.Request{
		ChannelID: job.BroadcasterID, RequestID: job.EventID, Text: strings.Repeat("é", tc.wantRunes),
		Style: job.Style, Voice: tc.wantVoice,
	}, req)
	assert.EqualValues(t, 1, f.recorded.Load())

	wantAttempts := int32(1)
	if tc.retry {
		wantAttempts++
	}
	assert.Equal(t, wantAttempts, f.attempts.Load())
	record, ok := f.records.Load(job.EventID)
	require.True(t, ok)
	assert.Equal(t, usersrpc.VoiceRecordRequest{
		UserID: job.BroadcasterID, RequestID: job.EventID, AudioTokens: 1, DurationMS: 1,
	}, record)
}

func (r testVoiceRig) assertDelivery(t *testing.T, job voiceplan.AlertJob) {
	t.Helper()
	ctx := t.Context()
	bucket, err := r.js.ObjectStore(ctx, "TTS_CLIPS")
	require.NoError(t, err)
	wav, err := bucket.GetBytes(ctx, "42/ev-12")
	require.NoError(t, err)
	assert.Equal(t, fakeWAV, wav)

	stream, err := r.js.Stream(ctx, "OVERLAY_TTS")
	require.NoError(t, err)
	info := stream.CachedInfo()
	require.EqualValues(t, 1, info.State.Msgs)
	msg, err := stream.GetMsg(ctx, info.State.LastSeq)
	require.NoError(t, err)
	var event voiceplan.OverlayEvent
	require.NoError(t, codec.Unmarshal(msg.Data, &event))

	assert.Equal(t, voiceplan.OverlayEvent{
		ClipKey: "42/ev-12", EventID: job.EventID, Kind: job.Kind, DurationMS: 1, ExpiresAt: event.ExpiresAt,
	}, event)
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), event.ExpiresAt, 15*time.Second)
}
