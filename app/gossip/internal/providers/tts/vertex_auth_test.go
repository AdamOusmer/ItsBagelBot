// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const vertexTestEmail = "tts@key-project.iam.gserviceaccount.com"

type vertexTestKey struct {
	public *rsa.PublicKey
	pem    string
}

func newVertexTestKey(t *testing.T) vertexTestKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return vertexTestKey{public: &key.PublicKey, pem: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
}

func (key vertexTestKey) credentials(t *testing.T, tokenURI string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "key-project", "client_email": vertexTestEmail,
		"private_key": key.pem, "token_uri": tokenURI,
	})
	require.NoError(t, err)
	return string(raw)
}

type vertexTestUpstreams struct {
	origin, tokenOrigin string
	model, token        http.Handler
}

func vertexTestConfig(t *testing.T, credentials string, upstreams vertexTestUpstreams) Config {
	t.Helper()
	modelURL := providertest.Upstream(t, upstreams.model)
	token := upstreams.token
	if token == nil {
		token = providertest.Respond(http.StatusOK, vertexTokenReply)
	}
	tokenURL := providertest.Upstream(t, token)
	return Config{
		APIKey: "gemini-test-fallback-key", Model: gossiprpc.TTSFlash, Voice: "Kore", RateLimit: 60, ChannelRateLimit: 12,
		Vertex: VertexConfig{
			Credentials: credentials,
			routeOrigin: func(origin string) string {
				if origin == cmp.Or(upstreams.tokenOrigin, "https://oauth2.googleapis.com") {
					return tokenURL
				}
				assert.Equal(t, cmp.Or(upstreams.origin, vertexGlobalOrigin), origin)
				return modelURL
			},
		},
	}
}

type vertexTokenCheck struct {
	path, audience string
	public         *rsa.PublicKey
}

func (check vertexTokenCheck) verify(t *testing.T, r *http.Request) {
	t.Helper()
	assert.Equal(t, "POST "+check.path, r.Method+" "+r.URL.RequestURI())
	assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
	if assert.NoError(t, r.ParseForm()) {
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.PostForm.Get("grant_type"))
		check.verifyAssertion(t, r.PostForm.Get("assertion"))
	}
}

func (check vertexTokenCheck) verifyAssertion(t *testing.T, assertion string) {
	t.Helper()
	parts := strings.Split(assertion, ".")
	if !assert.Len(t, parts, 3) {
		return
	}
	header, headerErr := base64.RawURLEncoding.DecodeString(parts[0])
	body, bodyErr := base64.RawURLEncoding.DecodeString(parts[1])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
	if !assert.NoError(t, errors.Join(headerErr, bodyErr, signatureErr)) {
		return
	}
	assert.JSONEq(t, `{"alg":"RS256","typ":"JWT"}`, string(header))
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	assert.NoError(t, rsa.VerifyPKCS1v15(check.public, crypto.SHA256, digest[:], signature))
	check.verifyClaims(t, body)
}

func (check vertexTokenCheck) verifyClaims(t *testing.T, body []byte) {
	t.Helper()
	var claims struct {
		IssuedAt int64 `json:"iat"`
	}
	if assert.NoError(t, json.Unmarshal(body, &claims)) {
		assert.InDelta(t, time.Now().Unix(), claims.IssuedAt, 10)
		want := fmt.Sprintf(`{"iss":%q,"scope":"https://www.googleapis.com/auth/cloud-platform","aud":%q,"iat":%d,"exp":%d}`,
			vertexTestEmail, check.audience, claims.IssuedAt, claims.IssuedAt+3600)
		assert.JSONEq(t, want, string(body))
	}
}

func TestVertexCachesTokenAcrossConcurrentSynthesis(t *testing.T) {
	const calls = 12
	key := newVertexTestKey(t)
	sequence := newTokenSequence()
	defer sequence.release()
	check := vertexTokenCheck{path: "/token", audience: "https://oauth2.googleapis.com/token", public: key.public}
	token := sequence.handler(t, check)
	model := providertest.Respond(http.StatusOK, vertexAudioResponse("audio/L16", "AAA=", "{}"))
	cfg := vertexTestConfig(t, key.credentials(t, ""), vertexTestUpstreams{token: token, model: model})
	p := New(cfg, providertest.Deps(nil))
	req := gossiprpc.Request{ChannelID: "42", Text: "hi"}
	handle := providertest.Endpoint(t, p, "synthesize")
	start := make(chan struct{})
	results := make(chan any, calls)
	for range calls {
		go func() {
			<-start
			results <- handle(t.Context(), req)
		}()
	}
	close(start)
	sequence.wait(t)
	for range calls {
		got := providertest.Decode[gossiprpc.TTSSynthesizeReply](t, <-results)
		assert.NotEmpty(t, got.Audio)
	}
	assert.NotEmpty(t, providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", req).Audio)
	assert.EqualValues(t, 1, sequence.hits.Load())
}

type vertexTokenSequence struct {
	started, duplicate chan struct{}
	unblock            chan struct{}
	releaseOnce        sync.Once
	hits               atomic.Int32
}

func newTokenSequence() *vertexTokenSequence {
	return &vertexTokenSequence{
		started: make(chan struct{}), duplicate: make(chan struct{}), unblock: make(chan struct{}),
	}
}

func (s *vertexTokenSequence) release() {
	s.releaseOnce.Do(func() { close(s.unblock) })
}

func (s *vertexTokenSequence) handler(t *testing.T, check vertexTokenCheck) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch s.hits.Add(1) {
		case 1:
			close(s.started)
		case 2:
			close(s.duplicate)
		}
		check.verify(t, r)
		<-s.unblock
		_, _ = io.WriteString(w, vertexTokenReply)
	}
}

func (s *vertexTokenSequence) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for token exchange")
	}
	select {
	case <-s.duplicate:
		t.Error("a second token exchange started while the first was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	s.release()
}

func TestVertexRefreshesWithinLeadTime(t *testing.T) {
	const tokenOrigin = "https://accounts.google.com"
	const tokenPath = "/o/oauth2/token"
	key := newVertexTestKey(t)
	check := vertexTokenCheck{path: tokenPath, audience: tokenOrigin + tokenPath, public: key.public}
	var hits atomic.Int32
	token := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		check.verify(t, r)
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":300}`, hits.Add(1))
	})
	model := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("Bearer token-%d", hits.Load()), r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, vertexAudioResponse("audio/L16", "AAA=", "{}"))
	})
	cfg := vertexTestConfig(t, key.credentials(t, check.audience), vertexTestUpstreams{tokenOrigin: tokenOrigin, token: token, model: model})
	p := New(cfg, providertest.Deps(nil))
	for range 2 {
		req := gossiprpc.Request{ChannelID: "42", Text: "hi"}
		assert.NotEmpty(t, providertest.Call[gossiprpc.TTSSynthesizeReply](t, p, "synthesize", req).Audio)
	}
	assert.EqualValues(t, 2, hits.Load())
}

func TestVertexTokenRejection(t *testing.T) {
	key := newVertexTestKey(t)
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			checkVertexTokenRejection(t, key, status)
		})
	}
}

func checkVertexTokenRejection(t *testing.T, key vertexTestKey, status int) {
	t.Helper()
	observed, logs := observer.New(zap.WarnLevel)
	deps := providertest.Deps(nil)
	deps.Log = zap.New(observed)
	var captured atomic.Pointer[string]
	token := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		assertion := r.PostForm.Get("assertion")
		captured.Store(&assertion)
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"error":"invalid_grant","error_description":%q}`, assertion)
	})
	cfg := vertexTestConfig(t, key.credentials(t, ""), vertexTestUpstreams{token: token, model: providertest.Forbid(t)})
	req := gossiprpc.Request{ChannelID: "42", Text: "hi"}
	got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, New(cfg, deps), "synthesize", req)
	assert.Equal(t, gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}, got)
	assert.Equal(t, 1, logs.FilterMessage("Vertex TTS credentials rejected").Len())
	assertion := captured.Load()
	if assert.NotNil(t, assertion) {
		assert.NotContains(t, fmt.Sprint(logs.All()), *assertion)
	}
}

func TestVertexRejectsUntrustedTokenURI(t *testing.T) {
	key := newVertexTestKey(t)
	for _, uri := range []string{"http://oauth2.googleapis.com/token", "https://oauth2.googleapis.com.example.com/token"} {
		t.Run(uri, func(t *testing.T) {
			upstream := providertest.Forbid(t)
			cfg := vertexTestConfig(t, key.credentials(t, uri), vertexTestUpstreams{model: upstream, token: upstream})
			req := gossiprpc.Request{ChannelID: "42", Text: "hi"}
			got := providertest.Call[gossiprpc.TTSSynthesizeReply](t, New(cfg, providertest.Deps(nil)), "synthesize", req)
			assert.Equal(t, gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}, got)
		})
	}
}
