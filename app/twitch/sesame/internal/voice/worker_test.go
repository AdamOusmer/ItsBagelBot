// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice_test

import (
	"cmp"
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/internal/voice"
	"ItsBagelBot/internal/domain/rpc"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	gossipSubject = "rpc.gossip.tts.synthesize"
	workerJobID   = "52321f6ed12e5e17e61656395631563a"
	workerTraceID = "5292b5d5721e379066e91a8897b7901a"
)

var fakeWAV = []byte("RIFF\x28\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\xc0\x5d\x00\x00\x80\xbb\x00\x00\x02\x00\x10\x00data\x04\x00\x00\x00\x00\x00\x00\x00")

type voiceRPCs struct {
	allowance usersrpc.VoiceCheckReply
	settings  overlaysrpc.TtsSettingsReply
	loseReply bool
	failure   *gossiprpc.TTSSynthesizeReply
	synthesis chan synthesisAttempt
	records   sync.Map
	traces    sync.Map
	checks    atomic.Int32
	attempts  atomic.Int32
	calls     atomic.Int32
	recorded  atomic.Int32
}

type synthesisAttempt struct {
	gossiprpc.Request
	started time.Time
}

func (r testVoiceRig) serve(t *testing.T, fake *voiceRPCs) {
	t.Helper()
	fake.synthesis = make(chan synthesisAttempt, 4)
	fakeServe(t, r.nc, usersrpc.VoiceCheckSubject, fake.check)
	fakeServe(t, r.nc, overlaysrpc.TtsSettingsSubject, fake.readSettings)
	fakeServe(t, r.nc, usersrpc.VoiceRecordSubject, fake.record)
	fakeServe(t, r.nc, gossipSubject, fake.synthesize)
}

func fakeServe[Req, Resp any](t *testing.T, nc *nats.Conn, subject string, handle func(context.Context, Req) Resp) {
	t.Helper()
	err := bus.QueueSubscribeJSONConcurrent(nc, bus.RPCSubscription{Subject: subject, QueueGroup: "fake-voice"}, time.Second, nil, zap.NewNop(), handle)
	require.NoError(t, err)
}

func (f *voiceRPCs) readSettings(ctx context.Context, _ overlaysrpc.TtsUserRequest) overlaysrpc.TtsSettingsReply {
	f.captureTrace(ctx, overlaysrpc.TtsSettingsSubject)
	return f.settings
}

func (f *voiceRPCs) check(ctx context.Context, _ usersrpc.VoiceCheckRequest) usersrpc.VoiceCheckReply {
	f.captureTrace(ctx, usersrpc.VoiceCheckSubject)
	f.checks.Add(1)
	return f.allowance
}

func (f *voiceRPCs) record(ctx context.Context, req usersrpc.VoiceRecordRequest) usersrpc.VoiceReply {
	f.captureTrace(ctx, usersrpc.VoiceRecordSubject)
	if _, existed := f.records.LoadOrStore(req.RequestID, req); !existed {
		f.recorded.Add(1)
	}
	first := f.attempts.Add(1) == 1
	if first && f.loseReply {
		return usersrpc.VoiceReply{Refusal: rpc.Refused(rpc.CodeUnavailable, "retry after commit")}
	}
	return usersrpc.VoiceReply{}
}

func (f *voiceRPCs) synthesize(ctx context.Context, req gossiprpc.Request) gossiprpc.TTSSynthesizeReply {
	f.captureTrace(ctx, gossipSubject)
	f.synthesis <- synthesisAttempt{Request: req, started: time.Now()}
	if f.calls.Add(1) == 1 && f.failure != nil {
		return *f.failure
	}
	return gossiprpc.TTSSynthesizeReply{Audio: fakeWAV, MimeType: "audio/wav", AudioTokens: 1, DurationMS: 1}
}

type voiceRPCTrace struct {
	subject string
	id      string
}

func (f *voiceRPCs) captureTrace(ctx context.Context, subject string) {
	id := newrelic.FromContext(ctx).GetTraceMetadata().TraceID
	f.traces.Store(voiceRPCTrace{subject: subject, id: id}, true)
}

func (f *voiceRPCs) assertTraces(t *testing.T, traceID string) {
	t.Helper()
	got := map[voiceRPCTrace]bool{}
	f.traces.Range(func(key, _ any) bool {
		got[key.(voiceRPCTrace)] = true
		return true
	})
	assert.Equal(t, map[voiceRPCTrace]bool{
		{subject: overlaysrpc.TtsSettingsSubject, id: traceID}: true,
		{subject: gossipSubject, id: traceID}:                  true,
		{subject: usersrpc.VoiceCheckSubject, id: traceID}:     true,
		{subject: usersrpc.VoiceRecordSubject, id: traceID}:    true,
	}, got)
}

type testVoiceRig struct {
	nc      *nats.Conn
	jobs    *bus.TTSJobs
	overlay *bus.Overlays
	js      jsapi.JetStream
}

func newTestVoiceRig(t *testing.T) testVoiceRig {
	t.Helper()
	nc := bustest.JetStream(t)
	jobs, err := bus.OpenTTSJobs(t.Context(), nc, 1)
	require.NoError(t, err)
	overlay, err := bus.OpenOverlays(t.Context(), nc, 1)
	require.NoError(t, err)
	js, err := jsapi.New(nc)
	require.NoError(t, err)
	return testVoiceRig{nc: nc, jobs: jobs, overlay: overlay, js: js}
}

func (r testVoiceRig) process(t *testing.T, ctx context.Context, job voiceplan.AlertJob) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core)
	worker := voice.Worker{RPC: r.nc, Gossip: gossipSubject, Overlay: r.overlay, Log: log}
	stop, err := r.jobs.Consume(t.Context(), worker.Process, 4, log)
	require.NoError(t, err)
	t.Cleanup(stop)

	pub := voice.Publisher{RPC: r.nc, Jobs: r.jobs}
	require.NoError(t, pub.Publish(ctx, job))
	require.NoError(t, pub.Publish(ctx, job))

	stream, err := r.js.Stream(t.Context(), bus.TTSJobsStream)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, err := stream.Info(t.Context())
		return err == nil && info.State.Msgs == 0 && info.State.LastSeq == 1
	}, 10*time.Second, 20*time.Millisecond)
	return logs
}

type workerCase struct {
	name      string
	wantDrop  string
	allowed   bool
	max       int
	remaining int64
	override  string
	wantVoice string
	wantRunes int
	retry     bool
	failure   *gossiprpc.TTSSynthesizeReply
	delay     time.Duration
}

func TestWorkerProcessesJetStreamVoiceJobs(t *testing.T) {
	const rateLimitDelay = 3 * time.Second
	cases := []workerCase{
		{name: "denied", wantDrop: "not_allowed", max: 5, remaining: 500},
		{name: "remaining below one second", wantDrop: "not_allowed", allowed: true, max: 5, remaining: 24},
		{name: "remaining budget and record retry", allowed: true, max: 5, remaining: 50, wantVoice: "Puck", wantRunes: 28, retry: true},
		{name: "clip length and override", allowed: true, max: 5, remaining: 500, override: "Zephyr", wantVoice: "Zephyr", wantRunes: 70},
		{name: "refused is acknowledged", wantDrop: "refused", allowed: true, max: 5, remaining: 500,
			failure: &gossiprpc.TTSSynthesizeReply{Error: "the speech provider refused this request", Reason: "refused"}},
		{name: "invalid text is acknowledged", wantDrop: "invalid", allowed: true, max: 5, remaining: 500,
			failure: &gossiprpc.TTSSynthesizeReply{Error: "invalid text: control characters are not allowed"}},
		{name: "rate limit honors retry after", allowed: true, max: 5, remaining: 500, wantVoice: "Puck", wantRunes: 70,
			failure: &gossiprpc.TTSSynthesizeReply{Error: "busy", RateLimited: true, RetryAfterMS: rateLimitDelay.Milliseconds()}, delay: rateLimitDelay},
		{name: "unavailable retries", allowed: true, max: 5, remaining: 500, wantVoice: "Puck", wantRunes: 70,
			failure: &gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}, delay: 2 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t, t.Context())
		})
	}
}

func TestWorkerContinuesOriginatingEventTraceAcrossRetry(t *testing.T) {
	ctx := tracedVoiceContext(t)
	tc := workerCase{allowed: true, max: 5, remaining: 50, wantVoice: "Puck", wantRunes: 28, retry: true}
	tc.run(t, ctx)
}

func tracedVoiceContext(t *testing.T) context.Context {
	t.Helper()
	app, err := newrelic.NewApplication(newrelic.ConfigRemoteParentSampled(newrelic.AlwaysOn), func(cfg *newrelic.Config) {
		cfg.ServerlessMode.Enabled = true
		cfg.ServerlessMode.AccountID = "123"
		cfg.ServerlessMode.TrustedAccountKey = "123"
		cfg.ServerlessMode.PrimaryAppID = "456"
		cfg.DistributedTracer.Enabled = true
	})
	require.NoError(t, err)
	monitor.Install(app)
	t.Cleanup(func() { monitor.Install(nil) })
	txn := app.StartTransaction("sesame")
	t.Cleanup(txn.End)
	parent := "00-" + workerTraceID + "-00f067aa0ba902b7-01"
	txn.AcceptDistributedTraceHeaders(newrelic.TransportQueue, http.Header{"Traceparent": {parent}})
	require.Equal(t, workerTraceID, txn.GetTraceMetadata().TraceID)
	ctx := newrelic.NewContext(t.Context(), txn)
	return bus.WithOriginTrace(ctx, bus.Metadata{"traceparent": parent})
}

func (tc workerCase) run(t *testing.T, ctx context.Context) {
	rig := newTestVoiceRig(t)
	fakes := &voiceRPCs{
		allowance: checkReply(tc), settings: settingsReply(tc),
		loseReply: tc.retry,
		failure:   tc.failure,
	}
	rig.serve(t, fakes)
	traceID := bus.OriginTraceID(ctx)
	job := voiceplan.AlertJob{
		EventID: cmp.Or(traceID, workerJobID), TraceID: traceID, BroadcasterID: "42", Kind: "sub", Text: strings.Repeat("é", 90),
		Style: "warm, friendly, upbeat", Voice: tc.override, CreatedAt: time.Now(),
	}
	logs := rig.process(t, ctx, job)
	tc.assertDrop(t, logs, job)
	tc.assertSynthesis(t, fakes, job)
	if tc.wantRunes == 0 {
		rig.assertNoClip(t, fakes)
		return
	}

	tc.assertRecord(t, fakes, job)
	rig.assertDelivery(t, job)
	if traceID != "" {
		fakes.assertTraces(t, traceID)
	}
}

func TestWorkerDropsInvalidNotificationID(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	job := voiceplan.AlertJob{EventID: "legacy/msg+==", TraceID: workerTraceID, BroadcasterID: "42", Kind: "cheer", Text: "Hello"}
	payload, err := codec.Marshal(job)
	require.NoError(t, err)
	worker := voice.Worker{Log: zap.New(core)}
	require.NoError(t, worker.Process(t.Context(), payload))

	entries := logs.FilterMessage("voice job dropped").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	assert.Equal(t, []any{"invalid", workerTraceID}, []any{fields["reason"], fields["trace_id"]})
}

func (tc workerCase) assertDrop(t *testing.T, logs *observer.ObservedLogs, job voiceplan.AlertJob) {
	t.Helper()
	drops := logs.FilterMessage("voice job dropped").All()
	if tc.wantDrop == "" {
		assert.Empty(t, drops)
		assert.Equal(t, 1, logs.FilterMessage("voice job delivered").Len())
		return
	}
	require.Len(t, drops, 1)
	fields := drops[0].ContextMap()
	assert.Equal(t, []any{tc.wantDrop, job.BroadcasterID, job.EventID, job.TraceID},
		[]any{fields["reason"], fields["broadcaster_id"], fields["event_id"], fields["trace_id"]})
}

func settingsReply(tc workerCase) overlaysrpc.TtsSettingsReply {
	return overlaysrpc.TtsSettingsReply{Configured: true,
		Settings: overlaysrpc.TtsSettings{Enabled: tc.allowed, Voice: "Puck", MaxSeconds: tc.max},
	}
}

func checkReply(tc workerCase) usersrpc.VoiceCheckReply {
	return usersrpc.VoiceCheckReply{
		Allowed: tc.allowed,
		VoiceReply: usersrpc.VoiceReply{VoiceAllowance: usersrpc.VoiceAllowance{
			RemainingTokens: tc.remaining,
		}},
	}
}

func (r testVoiceRig) assertNoClip(t *testing.T, f *voiceRPCs) {
	t.Helper()
	clip, err := r.overlay.GetClip(t.Context(), bus.OverlayClip{Key: "42/" + workerJobID})
	require.NoError(t, err)
	assert.Empty(t, clip.Key)
	assert.Zero(t, f.attempts.Load())
}

func (tc workerCase) wantSynthesisCalls() int {
	switch {
	case tc.delay > 0:
		return 2
	case tc.wantRunes > 0, tc.failure != nil:
		return 1
	default:
		return 0
	}
}

func (tc workerCase) assertSynthesis(t *testing.T, f *voiceRPCs, job voiceplan.AlertJob) {
	t.Helper()
	calls := tc.wantSynthesisCalls()
	checks := 0
	if tc.allowed {
		checks = max(1, calls)
	}
	assert.EqualValues(t, checks, f.checks.Load())
	require.Len(t, f.synthesis, calls)
	if tc.wantRunes == 0 {
		return
	}
	first := <-f.synthesis
	want := gossiprpc.Request{
		ChannelID: job.BroadcasterID, RequestID: job.EventID, Text: strings.Repeat("é", tc.wantRunes),
		Style: job.Style, Voice: tc.wantVoice,
	}
	assert.Equal(t, want, first.Request)
	if tc.delay > 0 {
		second := <-f.synthesis
		assert.Equal(t, want, second.Request)
		assert.GreaterOrEqual(t, second.started.Sub(first.started), tc.delay-25*time.Millisecond)
	}
}

func (tc workerCase) assertRecord(t *testing.T, f *voiceRPCs, job voiceplan.AlertJob) {
	t.Helper()
	wantAttempts := int32(1)
	if tc.retry {
		wantAttempts++
	}
	assert.Equal(t, [2]int32{1, wantAttempts}, [2]int32{f.recorded.Load(), f.attempts.Load()})
	record, ok := f.records.Load(job.EventID)
	require.True(t, ok)
	assert.Equal(t, usersrpc.VoiceRecordRequest{
		UserID: job.BroadcasterID, RequestID: job.EventID, AudioTokens: 1, DurationMS: 1,
	}, record)
}

func (r testVoiceRig) assertDelivery(t *testing.T, job voiceplan.AlertJob) {
	t.Helper()
	ctx := t.Context()
	key := job.BroadcasterID + "/" + job.EventID
	bucket, err := r.js.ObjectStore(ctx, bus.OverlaysMediaBucket)
	require.NoError(t, err)
	wav, err := bucket.GetBytes(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, fakeWAV, wav)

	stream, err := r.js.Stream(ctx, bus.OverlaysPremiumStream)
	require.NoError(t, err)
	info := stream.CachedInfo()
	require.EqualValues(t, 1, info.State.Msgs)
	msg, err := stream.GetMsg(ctx, info.State.LastSeq)
	require.NoError(t, err)
	assert.Equal(t, "overlays.premium.tts."+job.BroadcasterID, msg.Subject)
	assert.Equal(t, job.EventID, msg.Header.Get(nats.MsgIdHdr))
	var event voiceplan.OverlayEvent
	require.NoError(t, codec.Unmarshal(msg.Data, &event))

	assert.Equal(t, voiceplan.OverlayEvent{
		ClipKey: key, EventID: job.EventID, TraceID: job.TraceID, Kind: job.Kind, DurationMS: 1, ExpiresAt: event.ExpiresAt,
	}, event)
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), event.ExpiresAt, 15*time.Second)
}
