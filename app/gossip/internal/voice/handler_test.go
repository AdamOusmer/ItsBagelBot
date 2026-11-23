// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package voice_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providers/tts"
	"ItsBagelBot/app/gossip/internal/providertest"
	"ItsBagelBot/app/gossip/internal/voice"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	userID  = "42"
	eventID = "evt-1"
	waitFor = 5 * time.Second
	tick    = 10 * time.Millisecond
)

type rig struct {
	nc       *nats.Conn
	overlays *bus.Overlays
	jobs     *bus.GossipJobs
	handler  voice.Handler
	usage    chan usersrpc.VoiceRecordRequest
	events   *nats.Subscription
	upstream *atomic.Int32
}

func newRig(t *testing.T, upstream http.HandlerFunc) rig {
	t.Helper()
	ctx := t.Context()
	nc := bustest.JetStream(t)
	overlays, err := bus.OpenOverlays(ctx, nc, 1)
	require.NoError(t, err)
	jobs, err := bus.OpenGossipJobs(ctx, nc, 1)
	require.NoError(t, err)

	hits := &atomic.Int32{}
	counted := func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		upstream(w, r)
	}
	speech := tts.NewSynthesizer(tts.Config{
		BaseURL: providertest.Upstream(t, http.HandlerFunc(counted)), APIKey: "key",
		Model: gossiprpc.TTSFlash, Voice: "Kore", Temperature: 1.5,
	}, providertest.Deps(nil))

	usage := make(chan usersrpc.VoiceRecordRequest, 8)
	sub, err := nc.Subscribe(usersrpc.VoiceRecordSubject, func(msg *nats.Msg) {
		var req usersrpc.VoiceRecordRequest
		if codec.Unmarshal(msg.Data, &req) == nil {
			usage <- req
		}
		_ = msg.Respond([]byte("{}"))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	events, err := nc.SubscribeSync("overlays.premium.tts." + userID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = events.Unsubscribe() })

	handler := voice.Handler{
		RPC: nc, Speech: speech, Overlay: overlays, Log: zap.NewNop(),
		Safety: pkgvalkey.NewSpeechSafety(testLimits()), Store: valkeyStore(t),
	}
	return rig{nc: nc, overlays: overlays, jobs: jobs, handler: handler, usage: usage, events: events, upstream: hits}
}

func testLimits() pkgvalkey.SpeechLimits {
	return pkgvalkey.SpeechLimits{
		ViewerChannelStrikes: 2, ViewerChannelWindow: time.Minute, ViewerChannelBlock: time.Minute,
		ViewerGlobalStrikes: 1000, ViewerGlobalWindow: time.Minute, ViewerGlobalBlock: time.Minute,
		ChannelRefusals: 100000, ChannelWindow: time.Minute, ChannelPause: time.Minute,
		GlobalRefusals: 100000, GlobalWindow: time.Minute, GlobalCooldown: time.Minute,
	}
}

func valkeyStore(t *testing.T) valkey_go.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	store, err := pkgvalkey.NewClient(addr, os.Getenv("VALKEY_TEST_PASSWORD"))
	require.NoError(t, err)
	t.Cleanup(store.Close)
	return store
}

func audioUpstream(w http.ResponseWriter, _ *http.Request) {
	data := base64.StdEncoding.EncodeToString(testWAV(6000))
	_, _ = fmt.Fprintf(w, `{"steps":[{"type":"model_output","content":[{"type":"audio","data":%q}]}]}`, data)
}

func testWAV(dataSize int) []byte {
	wav := make([]byte, 44+dataSize)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 24000)
	binary.LittleEndian.PutUint32(wav[28:], 48000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(dataSize))
	return wav
}

func alertJob(text string) voiceplan.AlertJob {
	return voiceplan.AlertJob{
		EventID: eventID, TraceID: "trace-1", BroadcasterID: userID, Kind: voiceplan.SpeechChat,
		Text: text, Voice: "Kore", CreatedAt: time.Now(),
	}
}

func message(t *testing.T, job voiceplan.AlertJob) bus.GossipJobMessage {
	t.Helper()
	data, err := codec.Marshal(job)
	require.NoError(t, err)
	return bus.GossipJobMessage{Kind: bus.GossipJobTTS, UserID: job.BroadcasterID, Data: data, Delivered: 1}
}

func (r rig) requireDelivered(t *testing.T) {
	t.Helper()
	select {
	case req := <-r.usage:
		assert.Equal(t, usersrpc.VoiceRecordRequest{UserID: userID, RequestID: eventID, AudioTokens: req.AudioTokens, DurationMS: 125}, req)
		assert.Positive(t, req.AudioTokens)
	case <-time.After(waitFor):
		t.Fatal("usage was not recorded")
	}
	msg, err := r.events.NextMsg(waitFor)
	require.NoError(t, err)
	var event voiceplan.OverlayEvent
	require.NoError(t, codec.Unmarshal(msg.Data, &event))
	assert.Equal(t, userID+"/"+eventID, event.ClipKey)
	assert.Equal(t, int64(125), event.DurationMS)
	assert.True(t, event.ExpiresAt.After(time.Now()))
}

func TestConsumedJobStoresClipRecordsUsageAndPublishesOverlay(t *testing.T) {
	r := newRig(t, audioUpstream)
	ctx := t.Context()
	stop, err := r.jobs.Consume(ctx, r.handler.Handle, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(stop)

	require.NoError(t, r.jobs.Publish(ctx, bus.GossipJob{Kind: bus.GossipJobTTS, UserID: userID, EventID: eventID, Value: alertJob("hello chat")}))
	r.requireDelivered(t)

	clip, err := r.overlays.GetClip(ctx, bus.OverlayClip{Key: userID + "/" + eventID})
	require.NoError(t, err)
	assert.Equal(t, "125", clip.Metadata["duration_ms"])

	js, err := jsapi.NewWithDomain(r.nc, bus.JSDomain())
	require.NoError(t, err)
	stream, err := js.Stream(ctx, bus.GossipJobsStream)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, err := stream.Info(ctx)
		return err == nil && info.State.Msgs == 0
	}, waitFor, tick)
}

func TestExistingClipSkipsSynthesis(t *testing.T) {
	r := newRig(t, func(http.ResponseWriter, *http.Request) {})
	ctx := t.Context()
	_, err := r.overlays.PutClip(ctx, bus.OverlayClipData{
		OverlayClip: bus.OverlayClip{Key: userID + "/" + eventID, Metadata: map[string]string{"duration_ms": "125", "audio_tokens": "4"}},
		WAV:         testWAV(6000),
	})
	require.NoError(t, err)

	require.NoError(t, r.handler.Handle(ctx, message(t, alertJob("hello chat"))))
	r.requireDelivered(t)
	assert.Zero(t, r.upstream.Load())
}

func TestFailedSynthesisOutcomes(t *testing.T) {
	status := func(code int, header string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", header)
			w.WriteHeader(code)
		}
	}
	for _, tc := range []struct {
		name     string
		upstream http.HandlerFunc
		job      voiceplan.AlertJob
		check    func(*testing.T, error)
	}{
		{"rate limit retries after the upstream delay", status(http.StatusTooManyRequests, "2"), alertJob("hello"), func(t *testing.T, err error) {
			var retry bus.JobRetryError
			require.ErrorAs(t, err, &retry)
			assert.Equal(t, 2*time.Second, retry.Delay)
		}},
		{"refused request is dropped", status(http.StatusBadRequest, "0"), alertJob("hello"), func(t *testing.T, err error) {
			assert.ErrorIs(t, err, bus.ErrDropJob)
		}},
		{"unknown voice is dropped", audioUpstream, withVoice(alertJob("hello"), "Nobody"), func(t *testing.T, err error) {
			assert.ErrorIs(t, err, bus.ErrDropJob)
		}},
		{"blank text is dropped", audioUpstream, alertJob("  "), func(t *testing.T, err error) {
			assert.ErrorIs(t, err, bus.ErrDropJob)
		}},
		{"upstream outage retries", status(http.StatusInternalServerError, "0"), alertJob("hello"), func(t *testing.T, err error) {
			var retry bus.JobRetryError
			require.Error(t, err)
			assert.False(t, errors.As(err, &retry))
			assert.NotErrorIs(t, err, bus.ErrDropJob)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.upstream)
			tc.check(t, r.handler.Handle(context.Background(), message(t, tc.job)))
			assert.Empty(t, r.usage)
		})
	}
}

func withVoice(job voiceplan.AlertJob, name string) voiceplan.AlertJob {
	job.Voice = name
	return job
}
