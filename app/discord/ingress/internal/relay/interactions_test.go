// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package relay_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/ingress/internal/relay"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const timestamp = "1700000000"

func newInteractions(t *testing.T, rec *recorder) (*relay.Interactions, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	fetch := func(context.Context) (string, error) { return hex.EncodeToString(pub), nil }
	return relay.NewInteractions(&relay.Relay{REST: rec, Pub: rec}, fetch), priv
}

func signedRequest(priv ed25519.PrivateKey, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", strings.NewReader(body))
	sig := ed25519.Sign(priv, []byte(timestamp+body))
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
	req.Header.Set("X-Signature-Timestamp", timestamp)
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestInteractionsAcknowledgesAndPublishesSignedRequests(t *testing.T) {
	command := `{"type":2,"id":"int-1","token":"tok-1","guild_id":"g1","channel_id":"c1","member":{"user":{"id":"u1"}}}`
	published1 := published{ddiscord.SubjectEventInteraction, ddiscord.Event{
		Type: "INTERACTION_CREATE", GuildID: "g1", ChannelID: "c1", UserID: "u1", Raw: []byte(command),
	}}
	cases := []struct {
		name    string
		body    string
		pubErr  error
		status  int
		resp    string
		calls   []any
		wantIDs []string
	}{
		{name: "ping is answered with a pong and not published", body: `{"type":1,"id":"p1"}`, status: 200, resp: `{"type":1}`},
		{name: "command is published then deferred", body: command, status: 200, resp: `{"type":5}`, calls: []any{published1}, wantIDs: []string{"interaction-int-1"}},
		{name: "publish failure is a server error", body: command, pubErr: errors.New("bus down"), status: 500, wantIDs: []string{"interaction-int-1"}},
		{name: "undecodable body is a bad request", body: `not json`, status: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{pubErr: tc.pubErr}
			h, priv := newInteractions(t, rec)

			w := serve(h, signedRequest(priv, tc.body))

			assert.Equal(t, tc.status, w.Code)
			if tc.resp != "" {
				assert.JSONEq(t, tc.resp, w.Body.String())
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			} else {
				assert.NotContains(t, w.Body.String(), `"type"`)
				assert.NotEqual(t, "application/json", w.Header().Get("Content-Type"))
			}
			assert.Equal(t, tc.calls, rec.calls)
			assert.Equal(t, tc.wantIDs, rec.ids)
		})
	}
}

func TestInteractionsRejectsUnverifiedRequests(t *testing.T) {
	body := `{"type":2,"id":"int-1"}`
	_, other, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	cases := []struct {
		name string
		edit func(priv ed25519.PrivateKey) *http.Request
	}{
		{"signature from another key", func(ed25519.PrivateKey) *http.Request { return signedRequest(other, body) }},
		{"tampered body", func(priv ed25519.PrivateKey) *http.Request {
			req := signedRequest(priv, body)
			req.Body = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":2,"id":"int-2"}`)).Body
			return req
		}},
		{"missing signature headers", func(ed25519.PrivateKey) *http.Request {
			return httptest.NewRequest(http.MethodPost, "/discord/interactions", strings.NewReader(body))
		}},
		{"non-hex signature", func(priv ed25519.PrivateKey) *http.Request {
			req := signedRequest(priv, body)
			req.Header.Set("X-Signature-Ed25519", "zz")
			return req
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			h, priv := newInteractions(t, rec)

			w := serve(h, tc.edit(priv))

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Empty(t, rec.calls)
		})
	}
}

func TestInteractionsRetriesTheVerifyKeyFetchAfterAFailure(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	fetches := 0
	fetch := func(context.Context) (string, error) {
		fetches++
		if fetches == 1 {
			return "", errors.New("discord unreachable")
		}
		return hex.EncodeToString(pub), nil
	}
	rec := &recorder{}
	h := relay.NewInteractions(&relay.Relay{REST: rec, Pub: rec}, fetch)
	ping := `{"type":1,"id":"p1"}`

	assert.Equal(t, http.StatusServiceUnavailable, serve(h, signedRequest(priv, ping)).Code)
	assert.Equal(t, http.StatusOK, serve(h, signedRequest(priv, ping)).Code)
	assert.Equal(t, http.StatusOK, serve(h, signedRequest(priv, ping)).Code)
	assert.Equal(t, 2, fetches, "a fetched key is cached")
}
