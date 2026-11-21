// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package providers_test

import (
	"ItsBagelBot/pkg/codec"
	"net/http"
	"slices"
	"testing"

	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	disabledTTSWarning  = "tts provider disabled: GOOGLE_VERTEX_CREDENTIALS and GEMINI_API_KEY unset"
	vertexConfigWarning = "invalid Vertex TTS configuration"
)

type ttsSelectionCase struct {
	name, credentials, apiKey, warning, replyError string
	upstream                                       bool
}

func ttsSelectionCases() []ttsSelectionCase {
	const invalidCredentials = `{"type":"service_account","private_key":"secret"}`
	const unavailable = "speech synthesis is unavailable right now"
	return []ttsSelectionCase{
		{name: "disabled without credentials", warning: disabledTTSWarning},
		{name: "Gemini with API key", apiKey: "gemini-key", upstream: true, replyError: "speech provider returned no audio"},
		{name: "Vertex only", credentials: invalidCredentials, warning: vertexConfigWarning, replyError: unavailable},
		{name: "Vertex takes precedence", credentials: invalidCredentials, apiKey: "gemini-key", warning: vertexConfigWarning, replyError: unavailable},
	}
}

func TestTTSBackendSelection(t *testing.T) {
	for _, tc := range ttsSelectionCases() {
		t.Run(tc.name, func(t *testing.T) { checkTTSSelection(t, tc) })
	}
}

func checkTTSSelection(t *testing.T, tc ttsSelectionCase) {
	t.Helper()
	t.Setenv("GOOGLE_VERTEX_CREDENTIALS", tc.credentials)
	t.Setenv("GEMINI_API_KEY", tc.apiKey)
	t.Setenv("GEMINI_BASE_URL", providertest.Upstream(t, tc.gemini(t)))
	t.Setenv("GEMINI_TTS_TEMPERATURE", "0.75")
	deps := providertest.Deps(nil)
	observed, logs := observer.New(zap.WarnLevel)
	deps.Log = zap.New(observed)
	cfg, err := config.Load()
	require.NoError(t, err)
	providerList := providers.All(cfg, deps)
	idx := slices.IndexFunc(providerList, func(p provider.Provider) bool { return p.Name() == "tts" })
	if tc.warning != "" {
		assert.Equal(t, 1, logs.FilterMessage(tc.warning).Len())
	}
	if tc.warning == disabledTTSWarning {
		assert.Equal(t, -1, idx)
		return
	}
	if assert.NotEqual(t, -1, idx) {
		req := gossiprpc.Request{ChannelID: "42", Text: "hi"}
		got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, providerList[idx], "synthesize", req)
		assert.Equal(t, tc.replyError, got.Error)
	}
}

func (tc ttsSelectionCase) gemini(t *testing.T) http.HandlerFunc {
	t.Helper()
	if !tc.upstream {
		return providertest.Forbid(t)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/interactions", r.URL.Path)
		assert.Equal(t, tc.apiKey, r.Header.Get("x-goog-api-key"))
		var body struct {
			GenerationConfig struct {
				Temperature float64 `json:"temperature"`
			} `json:"generation_config"`
		}
		if assert.NoError(t, codec.NewDecoder(r.Body).Decode(&body)) {
			assert.Equal(t, 0.75, body.GenerationConfig.Temperature)
		}
		providertest.Respond(http.StatusOK, "{}")(w, r)
	}
}

func TestVertexConfigFromEnv(t *testing.T) {
	for _, tc := range []struct{ name, project, location, wantLocation string }{
		{name: "defaults", wantLocation: "global"},
		{name: "overrides", project: "billing-project", location: "europe-west1", wantLocation: "europe-west1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOOGLE_VERTEX_PROJECT", tc.project)
			t.Setenv("GOOGLE_VERTEX_LOCATION", tc.location)
			t.Setenv("GEMINI_TTS_TEMPERATURE", "1.5")
			cfg, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, []string{tc.project, tc.wantLocation}, []string{cfg.VertexProject, cfg.VertexLocation})
		})
	}
}
