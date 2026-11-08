// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package oauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeManagerRequiresExplicitNetworkPort(t *testing.T) {
	config := Config{AuthorizeEndpoint: "https://dashboard.example", TokenEndpoint: "https://auth.example", Platform: "macos", Store: &memoryStore{}, OpenBrowser: func(string) error { return nil }}
	if _, err := New(t.Context(), config); !errors.Is(err, ErrConfiguration) {
		t.Fatal("native manager silently constructed direct networking")
	}
}

func TestWorkerRejectsForeignTargetsBeforeDial(t *testing.T) {
	var calls atomic.Int32
	w, err := NewWorkerNetwork(WorkerNetworkConfig{Endpoint: "https://auth.example", TLSConfig: &tls.Config{RootCAs: x509.NewCertPool()}, DialContext: func(context.Context, string, string) (net.Conn, error) { calls.Add(1); return nil, ErrUnavailable }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	for _, request := range invalidNetworkRequests() {
		if _, err = w.Do(t.Context(), request); !errors.Is(err, ErrConfiguration) {
			t.Fatal("worker accepted an unauthorized native request")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected native request reached the TCP broker")
	}
}

func invalidNetworkRequests() []NetworkRequest {
	return []NetworkRequest{
		{Endpoint: "https://foreign.example" + TokenPath, Proof: "proof"},
		{Endpoint: "http://auth.example" + TokenPath, Proof: "proof"},
		{Endpoint: "https://auth.example" + TokenPath + "?token=secret", Proof: "proof"},
		{Endpoint: "https://auth.example" + TokenPath, Proof: "proof\r\nInjected: header"},
		{Endpoint: "https://auth.example" + TokenPath, Proof: "proof", Form: strings.Repeat("x", 4097)},
		{Endpoint: "https://auth.example" + TokenPath, Proof: "proof", AccessToken: "must-not-send"},
		{Endpoint: "https://auth.example" + RevokePath, Proof: "proof", Revoke: true},
		{Endpoint: "https://auth.example" + RevokePath, Proof: "proof", AccessToken: "access", Form: "must-be-empty", Revoke: true},
	}
}

func TestWorkerRequiresInjectedConnectionFactory(t *testing.T) {
	if _, err := NewWorkerNetwork(WorkerNetworkConfig{Endpoint: "https://auth.example"}); !errors.Is(err, ErrConfiguration) {
		t.Fatal("worker silently enabled direct DNS/TCP")
	}
}

func TestWorkerUsesInjectedTLSConnectionAndIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeReply(w, 200, validReply()) }))
	t.Cleanup(server.Close)
	var calls atomic.Int32
	dialer := &net.Dialer{}
	network, err := NewWorkerNetwork(WorkerNetworkConfig{Endpoint: server.URL, TLSConfig: server.Client().Transport.(*http.Transport).TLSClientConfig, DialContext: func(ctx context.Context, kind, address string) (net.Conn, error) {
		calls.Add(1)
		return dialer.DialContext(ctx, kind, address)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = network.Close() })
	reply, err := network.Do(t.Context(), NetworkRequest{Endpoint: server.URL + TokenPath, Proof: "proof", Form: "client_id=" + ClientID})
	if err != nil || reply.Token.Access != validReply().Access || calls.Load() != 1 {
		t.Fatal("worker bypassed injected connection factory or configured TLS trust")
	}
}

func TestWorkerDecoderDiscardsUnexpectedAndOversizedReplies(t *testing.T) {
	for _, body := range []string{strings.Repeat("x", maxTokenReply+1), `{"error":"upstream-secret-error"}`, `{"access_token":"token","token_type":"Bearer","expires_in":86400}`} {
		response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
		if parsed, err := DecodeTokenResponse(response); err == nil || parsed.Token.Access != "" {
			t.Fatal("unsafe HTTP response crossed the parsed network boundary")
		}
	}
}

func TestWorkerDecoderReturnsOnlyClassifiedErrorFields(t *testing.T) {
	response := &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}, "Dpop-Nonce": {"fresh-nonce"}}, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant","access_token":"must-not-escape","refresh_token":"must-not-escape"}`))}
	parsed, err := DecodeTokenResponse(response)
	if err != nil || parsed.Token.Error != "invalid_grant" || parsed.Token.Access != "" || parsed.Token.Refresh != "" || parsed.Nonce != "" {
		t.Fatal("error response leaked untrusted token fields or implied nonce retry")
	}
}

func TestWorkerDecoderRejectsExtensionsAndTrailingObjects(t *testing.T) {
	for _, body := range []string{
		`{"error":"invalid_grant","command":"whoami"}`,
		`{"error":"invalid_grant","public_ip":"retrieve"}`,
		`{"error":"invalid_grant","cli":{"write":"config"}}`,
		`{"error":"invalid_grant"} {"error":"server_error"}`,
	} {
		response := &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
		parsed, err := DecodeTokenResponse(response)
		if !errors.Is(err, ErrProtocol) || parsed != (NetworkResponse{}) {
			t.Fatal("unexpected JSON extension crossed the typed OAuth boundary")
		}
	}
}
