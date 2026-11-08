// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"ItsBagelBot/pkg/websocket/testkit"
)

func authorizedFixture(t *testing.T) *Client {
	t.Helper()
	service := testkit.Service{StreamPath: "/stream", Authorize: func(call testkit.Call) int {
		if http.Header(call.Headers).Get("Authorization") != "DPoP native" || http.Header(call.Headers).Get("DPoP") != "request-proof" {
			return 401
		}
		return 0
	}, HandleStream: func(ctx context.Context, _ testkit.Call, send func([]byte) error) error {
		if err := send([]byte{1, 2, 3}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}, Posts: map[string]func(context.Context, testkit.Call, []byte) int{"/control": func(_ context.Context, _ testkit.Call, body []byte) int {
		if string(body) != string([]byte{4, 5}) {
			return 400
		}
		return 204
	}}}
	server := testkit.New(t, service)
	client, err := NewClient(Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig, MaxMessageBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func TestDPoPTransportKeepsProofOutsideEventPayload(t *testing.T) {
	client := authorizedFixture(t)
	auth := Authorization{Scheme: "DPoP", Token: "native", Proof: "request-proof"}
	conn, reply, err := client.DialAuthorized(t.Context(), "/stream", auth)
	if err != nil || reply.Status != 101 {
		t.Fatal(reply, err)
	}
	defer conn.Close()
	var buffer [128]byte
	n, err := conn.Read(t.Context(), buffer[:])
	if err != nil || string(buffer[:n]) != string([]byte{1, 2, 3}) {
		t.Fatal("proof changed event data", err)
	}
	reply, err = client.PostAuthorized(t.Context(), "/control", auth, "application/protobuf", []byte{4, 5})
	if err != nil || reply.Status != 204 {
		t.Fatal(reply, err)
	}
}
func TestDPoPNonceChallengeDoesNotBecomeSignOut(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("DPoP-Nonce", "fresh-nonce")
		w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
		if r.URL.Path == "/challenge" {
			w.Header().Set("Content-Type", "text/html")
		}
		w.WriteHeader(400)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, TLSConfig: server.Client().Transport.(*http.Transport).TLSClientConfig, MaxMessageBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	auth := Authorization{Scheme: "DPoP", Token: "native", Proof: "stale-nonce-proof"}
	_, reply, err := client.DialAuthorized(t.Context(), "/stream", auth)
	if !errors.Is(err, ErrNonce) || reply.Nonce != "fresh-nonce" {
		t.Fatal(reply, err)
	}
	reply, err = client.PostAuthorized(t.Context(), "/control", auth, "application/protobuf", nil)
	if !errors.Is(err, ErrNonce) || reply.Nonce != "fresh-nonce" {
		t.Fatal(reply, err)
	}
	_, _, err = client.DialAuthorized(t.Context(), "/challenge", auth)
	if !errors.Is(err, ErrProxyResponse) {
		t.Fatal("HTML challenge accepted as nonce", err)
	}
}
func TestFreshNonceDoesNotOverrideInvalidToken(t *testing.T) {
	response := &http.Response{StatusCode: 401, Header: make(http.Header)}
	response.Header.Set("DPoP-Nonce", "fresh-nonce")
	response.Header.Set("WWW-Authenticate", `DPoP error="invalid_token"`)
	auth := Authorization{Scheme: "DPoP", Token: "revoked", Proof: "proof"}
	if nonceChallenge(auth, responseReply(response), response) {
		t.Fatal("revoked token misclassified as nonce challenge")
	}
	response.Header.Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
	response.Header.Add("DPoP-Nonce", "ambiguous-nonce")
	if nonceChallenge(auth, responseReply(response), response) {
		t.Fatal("ambiguous nonce accepted")
	}
}
func TestDPoPHasNoBearerFallback(t *testing.T) {
	for _, auth := range []Authorization{{Scheme: "DPoP", Token: "native"}, {Scheme: "Bearer", Token: "native", Proof: "proof"}, {Scheme: "DPoP", Token: "native", Proof: "bad\r\nproof"}, {Scheme: "other", Token: "native"}} {
		if _, err := authorizationHeaders(auth); err != ErrCredentials {
			t.Fatal("accepted invalid credentials")
		}
	}
}
