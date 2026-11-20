// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/providers/tts"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
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
		RateLimit:        60,
		ChannelRateLimit: 12,
	}
}

func TestSynthesize(t *testing.T) {
	for _, tc := range []struct {
		name        string
		req         gossiprpc.Request
		requestJSON string
		wantModel   gossiprpc.TTSModel
		durationMS  int64
	}{
		{
			name: "configured defaults and trimmed Unicode text",
			req:  gossiprpc.Request{ChannelID: "42", Text: "  aé界🙂  "},
			requestJSON: `{"model":"gemini-3.8-flash-tts","input":[{"type":"user_input","content":[{"type":"text","text":"aé界🙂"}]}],
				"response_format":{"type":"audio","mime_type":"audio/wav"},"generation_config":{"speech_config":[{"voice":"Kore"}]}}`,
			wantModel: gossiprpc.TTSFlash, durationMS: 125,
		},
		{
			name: "style annotation and overrides with a minute of WAV",
			req:  gossiprpc.Request{ChannelID: "42", Text: "aé界🙂", Voice: "Zephyr", Style: "friendly", Model: gossiprpc.TTSFlashLite},
			requestJSON: `{"model":"gemini-3.8-flash-lite-tts","input":[{"type":"user_input","content":[{"type":"text","text":"aé界🙂",
				"annotations":[{"type":"speech_metadata","style":"friendly"}]}]}],
				"response_format":{"type":"audio","mime_type":"audio/wav"},"generation_config":{"speech_config":[{"voice":"Zephyr"}]}}`,
			wantModel: gossiprpc.TTSFlashLite, durationMS: 60000,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wav := testWAV(int(tc.durationMS * 48))
			response := audioOutput(base64.StdEncoding.EncodeToString(wav))
			upstream := checkedRequest(t, tc.requestJSON, response)
			p := tts.New(configFor(t, upstream), providertest.Deps(nil))
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", tc.req)
			assert.True(t, bytes.Equal(wav, got.Audio), "expected unchanged WAV bytes")
			got.Audio = nil
			assert.Equal(t, gossiprpc.TTSSynthesizeReply{
				MimeType: "audio/wav", DurationMS: tc.durationMS, BilledCharacters: 4, Model: tc.wantModel,
			}, got)
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
		{"control character", gossiprpc.Request{ChannelID: "42", Text: "\nhi"}, "control characters"},
		{"unknown voice", gossiprpc.Request{ChannelID: "42", Text: "hi", Voice: "kore"}, "unknown voice"},
		{"overlong style", gossiprpc.Request{ChannelID: "42", Text: "hi", Style: strings.Repeat("é", 201)}, "invalid style"},
		{"unknown model", gossiprpc.Request{ChannelID: "42", Text: "hi", Model: "unknown"}, "unknown model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", tc.req)
			assert.Contains(t, got.Error, tc.want)
		})
	}
}

func TestSynthesizeUpstreamFailures(t *testing.T) {
	const testText = "private text for speech"
	echo := fmt.Sprintf(`{"error":{"message":%q}}`, testKey+" "+testText)
	for _, tc := range []struct {
		name   string
		status int
		retry  string
		body   string
		want   gossiprpc.TTSSynthesizeReply
		warn   bool
	}{
		{
			name: "rate limited", status: 429, retry: "7", body: echo,
			want: gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is busy right now, try again shortly", RateLimited: true, RetryAfterMS: 7000},
		},
		{
			name: "unauthorized", status: 401, body: echo, warn: true,
			want: gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"},
		},
		{
			name: "forbidden", status: 403, body: echo, warn: true,
			want: gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"},
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed, logs := observer.New(zap.DebugLevel)
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
		})
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

func TestSynthesizeChannelLimit(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := pkgvalkey.NewClient(addr, os.Getenv("VALKEY_TEST_PASSWORD"))
	require.NoError(t, err)
	t.Cleanup(client.Close)

	response := audioOutput(base64.StdEncoding.EncodeToString(testWAV(48)))
	upstream := providertest.NewSequence(t, providertest.Reply{Status: 200, Body: response})
	cfg := configFor(t, upstream)
	cfg.ChannelRateLimit = 1
	deps := providertest.Deps(nil)
	deps.Limiter = ratelimit.New(client)
	p := tts.New(cfg, deps)
	req := gossiprpc.Request{ChannelID: uuid.NewString(), Text: "hi"}
	first := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", req)
	assert.Empty(t, first.Error)
	second := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", req)
	assert.True(t, second.RateLimited)
	req.ChannelID = uuid.NewString()
	third := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", req)
	assert.Empty(t, third.Error)
	assert.Equal(t, 2, upstream.Hits())
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
