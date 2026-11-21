// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providers/tts"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const testKey = "private-gemini-test-key"

func configFor(t testing.TB, upstream http.Handler) tts.Config {
	return tts.Config{
		BaseURL:          providertest.Upstream(t, upstream),
		APIKey:           testKey,
		Model:            gossiprpc.TTSFlash,
		Voice:            "Kore",
		Temperature:      1.5,
		RateLimit:        60,
		ChannelRateLimit: 12,
	}
}

func TestSynthesize(t *testing.T) {
	for _, tc := range []struct {
		name        string
		req         gossiprpc.Request
		requestJSON string
		want        gossiprpc.TTSSynthesizeReply
		temperature float64
	}{
		{
			name: "shapes Unicode text before validation and billing",
			req:  gossiprpc.Request{ChannelID: "42", Text: "\n aé界🙂" + strings.Repeat(".", 501) + "\t\nbye <long pause>", Style: "\t \n"},
			requestJSON: `{"model":"gemini-3.8-flash-tts","input":[{"type":"user_input","content":[{"type":"text","text":"aé界🙂... bye"}]}],
				"response_format":{"type":"audio","mime_type":"audio/wav"},"generation_config":{"speech_config":[{"voice":"Kore"}],"temperature":1.5}}`,
			want:        gossiprpc.TTSSynthesizeReply{MimeType: "audio/wav", DurationMS: 125, BilledCharacters: 11, Model: gossiprpc.TTSFlash},
			temperature: 1.5,
		},
		{
			name: "style annotation and overrides with a minute of WAV",
			req:  gossiprpc.Request{ChannelID: "42", Text: "aé界🙂", Voice: "Zephyr", Style: "\t amical 🙂 \n", Model: gossiprpc.TTSFlashLite},
			requestJSON: `{"model":"gemini-3.8-flash-lite-tts","input":[{"type":"user_input","content":[{"type":"text","text":"aé界🙂",
				"annotations":[{"type":"speech_metadata","style":"amical 🙂"}]}]}],
				"response_format":{"type":"audio","mime_type":"audio/wav"},"generation_config":{"speech_config":[{"voice":"Zephyr"}],"temperature":2}}`,
			want:        gossiprpc.TTSSynthesizeReply{MimeType: "audio/wav", DurationMS: 60000, BilledCharacters: 4, StyleCharacters: 8, Model: gossiprpc.TTSFlashLite},
			temperature: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wav := testWAV(int(tc.want.DurationMS * 48))
			response := audioOutput(base64.StdEncoding.EncodeToString(wav))
			upstream := checkedRequest(t, tc.requestJSON, response)
			cfg := configFor(t, upstream)
			cfg.Temperature = tc.temperature
			p := tts.New(cfg, providertest.Deps(nil))
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", tc.req)
			assert.True(t, bytes.Equal(wav, got.Audio), "expected unchanged WAV bytes")
			got.Audio = nil
			assert.Equal(t, tc.want, got)
		})
	}
}

func checkedRequest(t testing.TB, requestJSON, response string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST /v1beta/interactions", r.Method+" "+r.URL.RequestURI())
		assert.Equal(t, testKey, r.Header.Get("x-goog-api-key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		if assert.NoError(t, err) {
			assert.JSONEq(t, requestJSON, string(body))
		}
		_, _ = io.WriteString(w, response)
	}
}

func TestSynthesizeRejectsInvalidRequests(t *testing.T) {
	p := tts.New(configFor(t, providertest.Forbid(t)), providertest.Deps(nil))
	for _, tc := range []struct {
		name string
		req  gossiprpc.Request
		want string
	}{
		{"missing channel", gossiprpc.Request{Text: "hi"}, "missing channel"},
		{"empty text", gossiprpc.Request{ChannelID: "42", Text: "  "}, "invalid text"},
		{"overlong text", gossiprpc.Request{ChannelID: "42", Text: strings.Repeat("é", 501)}, "invalid text"},
		{"tags only", gossiprpc.Request{ChannelID: "42", Text: "<laugh> <long pause> ..."}, "invalid text: expected 1 to 500 characters"},
		{"text control character", gossiprpc.Request{ChannelID: "42", Text: "hi\x00there"}, "invalid text: control characters"},
		{"text format character", gossiprpc.Request{ChannelID: "42", Text: "\u200bhi"}, "invalid text: control characters"},
		{"unknown voice", gossiprpc.Request{ChannelID: "42", Text: "hi", Voice: "kore"}, "unknown voice"},
		{"overlong style", gossiprpc.Request{ChannelID: "42", Text: "hi", Style: strings.Repeat("é", 201)}, "invalid style"},
		{"style control character", gossiprpc.Request{ChannelID: "42", Text: "hi", Style: "friendly\u0080voice"}, "invalid style: control characters"},
		{"style format character", gossiprpc.Request{ChannelID: "42", Text: "hi", Style: "friendly\u202e"}, "invalid style: control characters"},
		{"unknown model", gossiprpc.Request{ChannelID: "42", Text: "hi", Model: "unknown"}, "unknown model"},
		{"overlong request id", gossiprpc.Request{ChannelID: "42", Text: "hi", RequestID: strings.Repeat("a", 65)}, "invalid request_id"},
		{"invalid request id", gossiprpc.Request{ChannelID: "42", Text: "hi", RequestID: " retry:1"}, "invalid request_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", tc.req)
			assert.Contains(t, got.Error, tc.want)
		})
	}
}

const upstreamFailureText = "private text for speech"

type upstreamFailureCase struct {
	name   string
	status int
	retry  string
	body   string
	want   gossiprpc.TTSSynthesizeReply
	warn   bool
}

func upstreamFailureCases() []upstreamFailureCase {
	echo := fmt.Sprintf(`{"error":{"message":%q}}`, testKey+" "+upstreamFailureText)
	unavailable := gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}
	refused := gossiprpc.TTSSynthesizeReply{Error: "the speech provider refused this request", Reason: "refused"}
	return []upstreamFailureCase{
		{
			name: "JSON retry delay precedes header", status: http.StatusTooManyRequests, retry: "7",
			body: `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"RATE_LIMIT_EXCEEDED"},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"12.125s"}]}}`,
			want: busyReply(12125),
		},
		{
			name: "invalid JSON retry delay falls back to header", status: http.StatusTooManyRequests, retry: "7",
			body: `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"later"}]}}`,
			want: busyReply(7000),
		},
		{
			name: "rate limited without a retry hint", status: http.StatusTooManyRequests, body: echo, want: busyReply(0),
		},
		{
			name: "invalid key in details", status: http.StatusBadRequest, warn: true, want: unavailable,
			body: `{"error":{"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`,
		},
		{
			name: "expired key in reason", status: http.StatusBadRequest, warn: true, want: unavailable,
			body: `{"error":{"reason":"API_KEY_EXPIRED"}}`,
		},
		{
			name: "restricted key in status", status: http.StatusBadRequest, warn: true, want: unavailable,
			body: `{"error":{"status":"API_KEY_SERVICE_BLOCKED"}}`,
		},
		{
			name: "key mentioned in message", status: http.StatusBadRequest, warn: true, want: unavailable,
			body: fmt.Sprintf(`{"error":{"message":%q}}`, "API key not valid: "+testKey),
		},
		{
			name: "unauthorized", status: http.StatusUnauthorized, body: echo, warn: true, want: unavailable,
		},
		{
			name: "forbidden", status: http.StatusForbidden, body: echo, warn: true, want: unavailable,
		},
		{
			name: "other invalid argument", status: http.StatusBadRequest, body: echo, want: refused,
		},
		{
			name: "unprocessable request", status: http.StatusUnprocessableEntity, body: echo, want: refused,
		},
		{
			name: "provider outage", status: http.StatusServiceUnavailable, body: echo, want: unavailable,
		},
		{
			name: "missing model audio", status: 200,
			body: `{"steps":[{"type":"user_input","content":[{"type":"audio","data":"ignored"}]},{"type":"model_output","content":[{"type":"text"}]}]}`,
			want: gossiprpc.TTSSynthesizeReply{Error: "speech provider returned no audio"},
		},
		{
			name: "invalid base64", status: 200, body: audioOutput("invalid!"),
			want: gossiprpc.TTSSynthesizeReply{Error: "speech provider returned invalid audio"},
		},
		{
			name: "truncated WAV", status: 200, body: audioOutput(base64.StdEncoding.EncodeToString(testWAV(48)[:60])),
			want: gossiprpc.TTSSynthesizeReply{Error: "speech provider returned invalid audio"},
		},
		{
			name: "response exceeds HTTP body limit", status: 200, body: audioOutput(strings.Repeat("A", 4<<20)),
			want: gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"},
		},
	}
}

func TestSynthesizeUpstreamFailures(t *testing.T) {
	for _, tc := range upstreamFailureCases() {
		t.Run(tc.name, func(t *testing.T) { checkUpstreamFailure(t, tc) })
	}
}

func checkUpstreamFailure(t *testing.T, tc upstreamFailureCase) {
	const testText = upstreamFailureText
	observed, logs := observer.New(zap.WarnLevel)
	deps := providertest.Deps(nil)
	deps.Log = zap.New(observed)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", tc.retry)
		w.WriteHeader(tc.status)
		_, _ = io.WriteString(w, tc.body)
	})
	p := tts.New(configFor(t, upstream), deps)
	got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", gossiprpc.Request{ChannelID: "42", Text: testText})
	assert.Equal(t, tc.want, got)
	assert.Equal(t, tc.warn, logs.FilterMessage("Gemini TTS API key rejected").Len() > 0)
	assertLogsExclude(t, fmt.Sprint(logs.All()), []string{testKey, testText})
}

func busyReply(retryAfterMS int64) gossiprpc.TTSSynthesizeReply {
	return gossiprpc.TTSSynthesizeReply{
		Error: "speech synthesis is busy right now, try again shortly", RateLimited: true, RetryAfterMS: retryAfterMS,
	}
}

func assertLogsExclude(t testing.TB, logs string, excluded []string) {
	t.Helper()
	for _, value := range excluded {
		assert.NotContains(t, logs, value)
	}
}

func TestSynthesizeDoesNotFollowRedirects(t *testing.T) {
	target := providertest.Upstream(t, providertest.Forbid(t))
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	})
	p := tts.New(configFor(t, upstream), providertest.Deps(nil))
	got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", gossiprpc.Request{ChannelID: "42", Text: "hi"})
	assert.NotEmpty(t, got.Error)
}

func TestSynthesizeLimits(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := pkgvalkey.NewClient(addr, os.Getenv("VALKEY_TEST_PASSWORD"))
	require.NoError(t, err)
	t.Cleanup(client.Close)

	const globalBucketKey = "ratelimit:gossip:{tts}:global"
	kv := pkgvalkey.NewKV(client)
	for _, tc := range []struct {
		name                    string
		globalRate, channelRate float64
		wantHits                int
	}{
		{name: "channel budget", globalRate: 60, channelRate: 1, wantHits: 2},
		{name: "global budget", globalRate: 1, channelRate: 12, wantHits: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, kv.Del(t.Context(), globalBucketKey))
			t.Cleanup(func() { assert.NoError(t, kv.Del(context.Background(), globalBucketKey)) })
			response := audioOutput(base64.StdEncoding.EncodeToString(testWAV(48)))
			upstream := providertest.NewSequence(t, providertest.Reply{Status: http.StatusOK, Body: response})
			cfg := configFor(t, upstream)
			cfg.RateLimit, cfg.ChannelRateLimit = tc.globalRate, tc.channelRate
			deps := providertest.Deps(nil)
			deps.Limiter = ratelimit.New(client)
			p := tts.New(cfg, deps)
			req := gossiprpc.Request{ChannelID: uuid.NewString(), Text: "hi"}
			call := func() gossiprpc.TTSSynthesizeReply {
				return providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", req)
			}

			require.Empty(t, call().Error)
			checkLocalLimit(t, call())
			req.ChannelID = uuid.NewString()
			other := call()
			if tc.wantHits == 1 {
				checkLocalLimit(t, other)
			} else {
				assert.Empty(t, other.Error)
			}
			assert.Equal(t, tc.wantHits, upstream.Hits())
		})
	}
}

func checkLocalLimit(t *testing.T, got gossiprpc.TTSSynthesizeReply) {
	t.Helper()
	assert.Positive(t, got.RetryAfterMS)
	assert.LessOrEqual(t, got.RetryAfterMS, int64(60000))
	assert.Equal(t, busyReply(got.RetryAfterMS), got)
}

func TestSynthesizeInvalidConfiguredDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, voice, wantVoice, warning string
		model, wantModel                gossiprpc.TTSModel
	}{
		{"invalid voice", "unknown", "Kore", "invalid GEMINI_TTS_VOICE, using default", gossiprpc.TTSFlashLite, gossiprpc.TTSFlashLite},
		{"invalid model", "Puck", "Puck", "invalid GEMINI_TTS_MODEL, using default", "unknown", gossiprpc.TTSFlash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed, logs := observer.New(zap.WarnLevel)
			deps := providertest.Deps(nil)
			deps.Log = zap.New(observed)
			requestJSON := fmt.Sprintf(`{"model":%q,"input":[{"type":"user_input","content":[{"type":"text","text":"hi"}]}],
				"response_format":{"type":"audio","mime_type":"audio/wav"},"generation_config":{"speech_config":[{"voice":%q}],"temperature":1.5}}`, tc.wantModel, tc.wantVoice)
			response := audioOutput(base64.StdEncoding.EncodeToString(testWAV(48)))
			cfg := configFor(t, checkedRequest(t, requestJSON, response))
			cfg.Voice, cfg.Model = tc.voice, tc.model
			p := tts.New(cfg, deps)

			voices := providertest.Call[gossiprpc.TTSVoicesReply](t, p, "voices", gossiprpc.Request{})
			assert.Equal(t, tc.wantModel, voices.Model)
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", gossiprpc.Request{ChannelID: "42", Text: "hi"})
			assert.Equal(t, []any{tc.wantModel, ""}, []any{got.Model, got.Error})
			assert.Equal(t, 1, logs.FilterMessage(tc.warning).Len())
		})
	}
}

func TestSynthesizeMetersUsage(t *testing.T) {
	wav := testWAV(48)
	response := strings.TrimSuffix(audioOutput(base64.StdEncoding.EncodeToString(wav)), "}")
	for _, tc := range []struct {
		name, fields string
		audio, total int64
	}{
		{"interaction usage", `,"usage":{"total_output_tokens":12,"total_tokens":40}`, 12, 40},
		{"audio modality", `,"usage":{"total_output_tokens":30,"output_tokens_by_modality":[{"modality":"text","tokens":12},{"modality":"audio","tokens":18}],"total_tokens":40}`, 18, 40},
		{"usage metadata fallback", `,"usage":null,"usage_metadata":{"output_tokens":16,"total_tokens":42}`, 16, 42},
		{"camel case metadata", `,"usageMetadata":{"candidatesTokenCount":11,"totalTokenCount":35}`, 11, 35},
		{"invalid output with valid total", `,"usage":{"total_output_tokens":"unknown","total_tokens":40}`, 0, 40},
		{"missing usage", "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := providertest.Respond(http.StatusOK, response+tc.fields+"}")
			p := tts.New(configFor(t, upstream), providertest.Deps(nil))
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", gossiprpc.Request{ChannelID: "42", Text: "hi"})
			assert.Equal(t, wav, got.Audio)
			got.Audio = nil
			raw, err := codec.Marshal(got)
			require.NoError(t, err)
			want := fmt.Sprintf(`{"mime_type":"audio/wav","duration_ms":1,"billed_characters":2,"style_characters":0,
				"audio_tokens":%d,"total_tokens":%d,"model":"gemini-3.8-flash-tts"}`, tc.audio, tc.total)
			assert.JSONEq(t, want, string(raw))
		})
	}
}

func TestSynthesizeCoalescesConcurrentRequestIDs(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	var hits atomic.Int32
	response := audioOutput(base64.StdEncoding.EncodeToString(testWAV(48)))
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			close(started)
			<-release
		}
		_, _ = io.WriteString(w, response)
	})
	p := tts.New(configFor(t, upstream), providertest.Deps(nil))
	defer unblock()
	handle := providertest.Endpoint(t, p, "synthesize")
	start := func(req gossiprpc.Request) <-chan any {
		result := make(chan any, 1)
		go func() { result <- handle(t.Context(), req) }()
		return result
	}

	req := gossiprpc.Request{ChannelID: "42", RequestID: "A_0-" + strings.Repeat("z", 60), Text: "hi"}
	first := start(req)
	await(t, started)
	retry := start(req)
	otherChannel := req
	otherChannel.ChannelID = "43"
	otherID := req
	otherID.RequestID = "other"
	for _, distinct := range []gossiprpc.Request{otherChannel, otherID} {
		got := providertest.Decode[gossiprpc.TTSSynthesizeReply](t, await(t, start(distinct)))
		require.Empty(t, got.Error)
	}
	select {
	case <-retry:
		t.Fatal("matching retry completed while the original call was still blocked")
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	original := providertest.Decode[gossiprpc.TTSSynthesizeReply](t, await(t, first))
	assert.NotEmpty(t, original.Audio)
	assert.Equal(t, original, await(t, retry))
	assert.EqualValues(t, 3, hits.Load())

	assert.Equal(t, original, handle(t.Context(), req))
	assert.EqualValues(t, 4, hits.Load())
}

func await[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for synthesis")
		var zero T
		return zero
	}
}

func TestVoices(t *testing.T) {
	cfg := configFor(t, providertest.Forbid(t))
	cfg.Model = gossiprpc.TTSFlashLite
	p := tts.New(cfg, providertest.Deps(nil))
	got := providertest.Call[gossiprpc.TTSVoicesReply](t, p, "voices", gossiprpc.Request{})
	assert.Equal(t, gossiprpc.TTSVoicesReply{
		Voices: strings.Fields("Zephyr Puck Charon Kore Fenrir Leda Orus Aoede Callirrhoe Autonoe Enceladus Iapetus Umbriel Algieba Despina Erinome Algenib Rasalgethi Laomedeia Achernar Alnilam Schedar Gacrux Pulcherrima Achird Zubenelgenubi Vindemiatrix Sadachbia Sadaltager Sulafat"),
		Model:  gossiprpc.TTSFlashLite,
	}, got)
}

func audioOutput(data string) string {
	return fmt.Sprintf(`{"steps":[{"type":"thought"},{"type":"model_output","content":[{"type":"text"},{"type":"audio","data":%q}]}]}`, data)
}

func testWAV(dataSize int) []byte {
	wav := make([]byte, 54+dataSize)
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
	copy(wav[36:], "JUNK")
	binary.LittleEndian.PutUint32(wav[40:], 1)
	copy(wav[46:], "data")
	binary.LittleEndian.PutUint32(wav[50:], uint32(dataSize))
	return wav
}
