// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	vertexTestToken       = "private-vertex-test-token"
	vertexTokenReply      = `{"access_token":"private-vertex-test-token","expires_in":3600}`
	vertexGlobalOrigin    = "https://aiplatform.googleapis.com"
	vertexSynthesisPrompt = "aé界🙂"
)

type vertexSynthesisCase struct {
	name, location, origin, project string
	req                             gossiprpc.Request
	requestJSON, usageJSON, media   string
	rate                            uint32
	temperature                     float64
	want                            gossiprpc.TTSSynthesizeReply
}

func vertexSynthesisCases() []vertexSynthesisCase {
	const plain = `{"contents":[{"role":"user","parts":[{"text":"aé界🙂"}]}],"generationConfig":{
		"responseModalities":["AUDIO"],"temperature":1.5,"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Kore"}}}}}`
	const styled = `{"contents":[{"role":"user","parts":[{"text":"aé界🙂","speechMetadata":{"style":"amical 🙂"}}]}],
		"generationConfig":{"responseModalities":["AUDIO"],"temperature":0,"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Zephyr"}}}}}`
	return []vertexSynthesisCase{
		{
			name: "global defaults", origin: vertexGlobalOrigin, temperature: 1.5,
			req:         gossiprpc.Request{ChannelID: "42", Text: "\n " + vertexSynthesisPrompt + " \t", Style: "  "},
			requestJSON: plain, media: "audio/L16;codec=pcm", rate: 24000,
			usageJSON: `{"promptTokenCount":8,"candidatesTokenCount":18,"totalTokenCount":26}`,
			want: gossiprpc.TTSSynthesizeReply{
				MimeType: "audio/wav", DurationMS: 20, BilledCharacters: 4, AudioTokens: 18, TotalTokens: 26, Model: gossiprpc.TTSFlash,
			},
		},
		{
			name: "regional model override with style and zero temperature", location: "us-central1", origin: "https://us-central1-aiplatform.googleapis.com",
			project: "billing-project", media: "audio/L16;codec=pcm;rate=16000", rate: 16000,
			req:         gossiprpc.Request{ChannelID: "42", Text: vertexSynthesisPrompt, Style: "\t amical 🙂 \n", Voice: "Zephyr", Model: gossiprpc.TTSFlashLite},
			requestJSON: styled,
			usageJSON: `{"promptTokenCount":8,"candidatesTokenCount":20,"totalTokenCount":30,
				"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":3},{"modality":"AUDIO","tokenCount":17}]}`,
			want: gossiprpc.TTSSynthesizeReply{
				MimeType: "audio/wav", DurationMS: 30, BilledCharacters: 4, StyleCharacters: 8, AudioTokens: 17, TotalTokens: 30, Model: gossiprpc.TTSFlashLite,
			},
		},
		{
			name: "US multi-region", location: "us", origin: "https://aiplatform.us.rep.googleapis.com", temperature: 1.5,
			req:         gossiprpc.Request{ChannelID: "42", Text: vertexSynthesisPrompt},
			requestJSON: plain, media: "audio/L16;codec=pcm;rate=24000", rate: 24000,
			usageJSON: `{"promptTokenCount":9,"candidatesTokenCount":2,"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":2}]}`,
			want: gossiprpc.TTSSynthesizeReply{
				MimeType: "audio/wav", DurationMS: 20, BilledCharacters: 4, TotalTokens: 11, Model: gossiprpc.TTSFlash,
			},
		},
		{
			name: "EU multi-region", location: "eu", origin: "https://aiplatform.eu.rep.googleapis.com", temperature: 1.5,
			req:         gossiprpc.Request{ChannelID: "42", Text: vertexSynthesisPrompt},
			requestJSON: plain, media: "audio/L16", rate: 24000, usageJSON: `{}`,
			want: gossiprpc.TTSSynthesizeReply{MimeType: "audio/wav", DurationMS: 20, BilledCharacters: 4, Model: gossiprpc.TTSFlash},
		},
	}
}

func TestVertexWAVPassthrough(t *testing.T) {
	key := newVertexTestKey(t)
	pcm := bytes.Repeat([]byte{0x34, 0x92}, 480)
	wav, _ := wrapPCM(pcm, 24000)
	response := vertexAudioResponse("audio/wav", base64.StdEncoding.EncodeToString(wav), `{"candidatesTokenCount":5}`)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, response) })
	cfg := vertexTestConfig(t, key.credentials(t, ""), vertexTestUpstreams{origin: vertexGlobalOrigin, model: upstream})
	got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, New(cfg, providertest.Deps(nil)), "synthesize", gossiprpc.Request{ChannelID: "42", Text: "hi"})
	assert.Equal(t, []any{"", wav, int64(20)}, []any{got.Error, got.Audio, got.DurationMS})
}

func TestVertexSynthesis(t *testing.T) {
	key := newVertexTestKey(t)
	for _, tc := range vertexSynthesisCases() {
		t.Run(tc.name, func(t *testing.T) {
			checkVertexSynthesis(t, key.credentials(t, ""), tc)
		})
	}
}

func checkVertexSynthesis(t *testing.T, credentials string, tc vertexSynthesisCase) {
	t.Helper()
	pcm := bytes.Repeat([]byte{0x34, 0x92}, 480)
	response := vertexAudioResponse(tc.media, base64.StdEncoding.EncodeToString(pcm), tc.usageJSON)
	upstream := tc.upstream(t, response)
	cfg := vertexTestConfig(t, credentials, vertexTestUpstreams{origin: tc.origin, model: upstream})
	cfg.Vertex.Project, cfg.Vertex.Location = tc.project, tc.location
	cfg.Temperature = tc.temperature
	p := New(cfg, providertest.Deps(nil))
	got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", tc.req)
	checkVertexWAV(t, got.Audio, pcm, tc.rate)
	got.Audio = nil
	assert.Equal(t, tc.want, got)
}

func (tc vertexSynthesisCase) upstream(t *testing.T, response string) http.HandlerFunc {
	t.Helper()
	project, location := tc.project, tc.location
	if project == "" {
		project = "key-project"
	}
	if location == "" {
		location = "global"
	}
	path := fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent", project, location, tc.want.Model)
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST "+path, r.Method+" "+r.URL.RequestURI())
		assert.Equal(t, []string{"Bearer " + vertexTestToken, "application/json", ""},
			[]string{r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("x-goog-api-key")})
		body, err := io.ReadAll(r.Body)
		if assert.NoError(t, err) {
			assert.JSONEq(t, tc.requestJSON, string(body))
		}
		_, _ = io.WriteString(w, response)
	}
}

func checkVertexWAV(t *testing.T, wav, pcm []byte, rate uint32) {
	t.Helper()
	require.Len(t, wav, 44+len(pcm))
	le := binary.LittleEndian
	assert.Equal(t, []any{
		"RIFF", "WAVEfmt ", "data",
		uint32(len(wav) - 8), uint32(16), rate, rate * 2, uint32(len(pcm)),
		uint16(1), uint16(1), uint16(2), uint16(16),
	}, []any{
		string(wav[:4]), string(wav[8:16]), string(wav[36:40]),
		le.Uint32(wav[4:]), le.Uint32(wav[16:]), le.Uint32(wav[24:]), le.Uint32(wav[28:]), le.Uint32(wav[40:]),
		le.Uint16(wav[20:]), le.Uint16(wav[22:]), le.Uint16(wav[32:]), le.Uint16(wav[34:]),
	})
	assert.Equal(t, pcm, wav[44:])
}

func vertexAudioResponse(media, data, usage string) string {
	return fmt.Sprintf(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"metadata"},
		{"inlineData":{"mimeType":"application/octet-stream","data":"aWdub3JlZA=="}},
		{"inlineData":{"mimeType":%q,"data":%q}}]}}],"usageMetadata":%s}`, media, data, usage)
}

type vertexFailureCase struct {
	name, body, retry string
	status            int
	want              gossiprpc.TTSSynthesizeReply
	warning           bool
}

func vertexFailureCases() []vertexFailureCase {
	refused := gossiprpc.TTSSynthesizeReply{Error: "the speech provider refused this request", Reason: "refused"}
	unavailable := gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}
	noAudio := gossiprpc.TTSSynthesizeReply{Error: "speech provider returned no audio"}
	invalid := gossiprpc.TTSSynthesizeReply{Error: "speech provider returned invalid audio"}
	busy := gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is busy right now, try again shortly", RateLimited: true, RetryAfterMS: 2125}
	headerBusy := busy
	headerBusy.RetryAfterMS = 3000
	echo := fmt.Sprintf(`{"error":{"message":%q}}`, vertexTestToken)
	return []vertexFailureCase{
		{name: "prompt blocked", body: `{"promptFeedback":{"blockReason":"SAFETY"}}`, status: 200, want: refused},
		{name: "candidate refused", body: `{"candidates":[{"finishReason":"PROHIBITED_CONTENT"}]}`, status: 200, want: refused},
		{name: "only first candidate", body: `{"candidates":[{"finishReason":"STOP"},
			{"content":{"parts":[{"inlineData":{"data":"AAA="}}]}}]}`, status: 200, want: noAudio},
		{name: "invalid base64", body: vertexAudioResponse("audio/L16", "invalid!", "{}"), status: 200, want: invalid},
		{name: "partial PCM frame", body: vertexAudioResponse("audio/L16", "AA==", "{}"), status: 200, want: invalid},
		{name: "invalid sample rate", body: vertexAudioResponse("audio/L16;rate=0", "AAA=", "{}"), status: 200, want: invalid},
		{name: "oversized response", body: vertexAudioResponse("audio/L16", strings.Repeat("A", 4<<20), "{}"), status: 200, want: unavailable},
		{name: "invalid request", body: echo, status: 400, want: refused},
		{name: "unauthorized", body: echo, status: 401, want: unavailable, warning: true},
		{name: "forbidden", body: echo, status: 403, want: unavailable, warning: true},
		{name: "capacity retry info", status: 429, retry: "3", want: busy,
			body: `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"2.125s"}]}}`},
		{name: "capacity retry header", status: 429, retry: "3", body: echo, want: headerBusy},
		{name: "backend unavailable", body: echo, status: 503, want: unavailable},
	}
}

func TestVertexFailures(t *testing.T) {
	credentials := newVertexTestKey(t).credentials(t, "")
	for _, tc := range vertexFailureCases() {
		t.Run(tc.name, func(t *testing.T) { checkVertexFailure(t, credentials, tc) })
	}
}

func checkVertexFailure(t *testing.T, credentials string, tc vertexFailureCase) {
	t.Helper()
	observed, logs := observer.New(zap.WarnLevel)
	deps := providertest.Deps(nil)
	deps.Log = zap.New(observed)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", tc.retry)
		providertest.Respond(tc.status, tc.body)(w, r)
	})
	cfg := vertexTestConfig(t, credentials, vertexTestUpstreams{model: upstream})
	req := gossiprpc.Request{ChannelID: "42", Text: vertexSynthesisPrompt}
	got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, New(cfg, deps), "synthesize", req)
	assert.Equal(t, tc.want, got)
	assert.Equal(t, tc.warning, logs.FilterMessage("Vertex TTS credentials rejected").Len() == 1)
	assert.NotContains(t, fmt.Sprint(logs.All()), vertexTestToken)
}
