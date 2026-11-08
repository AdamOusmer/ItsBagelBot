// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package websocket

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/pkg/websocket/testkit"
)

type testPeer struct {
	call   testkit.Call
	frames chan []byte
}
type testPost struct {
	call testkit.Call
	body []byte
}

func wsFixture(t *testing.T, text bool, limit int) (*Client, *testkit.Server, chan *testPeer, chan testPost) {
	t.Helper()
	peers := make(chan *testPeer, 4)
	posts := make(chan testPost, 4)
	server := testkit.New(t, testkit.Service{StreamPath: "/stream", Subprotocol: "bagel.bridge.v1", TextFrames: text, Authorize: func(call testkit.Call) int {
		if call.Token != "native" {
			return 401
		}
		return 0
	}, HandleStream: func(ctx context.Context, call testkit.Call, send func([]byte) error) error {
		peer := &testPeer{call: call, frames: make(chan []byte, 4)}
		select {
		case peers <- peer:
		case <-ctx.Done():
			return ctx.Err()
		}
		return sendTestFrames(ctx, peer, send)
	}, Posts: map[string]func(context.Context, testkit.Call, []byte) int{"/control": func(ctx context.Context, call testkit.Call, body []byte) int {
		select {
		case posts <- testPost{call: call, body: body}:
			return 204
		case <-ctx.Done():
			return 503
		}
	}}})
	client, err := NewClient(Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig, MaxMessageBytes: limit, Subprotocol: "bagel.bridge.v1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, server, peers, posts
}
func sendTestFrames(ctx context.Context, peer *testPeer, send func([]byte) error) error {
	for {
		select {
		case frame := <-peer.frames:
			if err := send(frame); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func wsPeer(t *testing.T, peers chan *testPeer) *testPeer {
	t.Helper()
	select {
	case peer := <-peers:
		return peer
	case <-time.After(time.Second):
		t.Fatal("binary stream not opened")
		return nil
	}
}
func wsDial(t *testing.T, client *Client) *Conn {
	t.Helper()
	conn, err := client.Dial(t.Context(), "/stream", "native")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
func TestBinaryReadOwnershipAndHTTPSControl(t *testing.T) {
	client, _, peers, posts := wsFixture(t, false, 128)
	conn := wsDial(t, client)
	peer := wsPeer(t, peers)
	peer.frames <- []byte("first")
	var first, second [128]byte
	n, err := conn.Read(t.Context(), first[:])
	if err != nil {
		t.Fatal(err)
	}
	peer.frames <- []byte("second")
	if _, err = conn.Read(t.Context(), second[:]); err != nil {
		t.Fatal(err)
	}
	if string(first[:n]) != "first" {
		t.Fatal("next read reused caller-owned buffer")
	}
	code, err := client.Post(t.Context(), "/control", "native", "application/x-protobuf", []byte{1, 2, 3})
	if err != nil || code != 204 {
		t.Fatalf("control status %d: %v", code, err)
	}
	post := <-posts
	if post.call.Token != "native" || post.call.ContentType != "application/x-protobuf" || string(post.body) != string([]byte{1, 2, 3}) {
		t.Fatal("HTTPS control changed metadata or bytes")
	}
}
func TestBinaryMessageBoundsAndType(t *testing.T) {
	for _, example := range []struct {
		name string
		text bool
		size int
		want error
	}{{"exact", false, 8, nil}, {"oversized", false, 9, ErrTooLarge}, {"text", true, 1, ErrMessageType}} {
		t.Run(example.name, func(t *testing.T) {
			client, _, peers, _ := wsFixture(t, example.text, 8)
			conn := wsDial(t, client)
			peer := wsPeer(t, peers)
			peer.frames <- []byte(strings.Repeat("x", example.size))
			var buffer [8]byte
			n, err := conn.Read(t.Context(), buffer[:])
			if !errors.Is(err, example.want) {
				t.Fatalf("message bound/type: %v", err)
			}
			if err == nil && n != example.size {
				t.Fatal("exact-boundary payload truncated")
			}
		})
	}
}
func TestReadCancellationAndClientClose(t *testing.T) {
	client, _, peers, _ := wsFixture(t, false, 128)
	a := wsDial(t, client)
	_ = wsPeer(t, peers)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	var buffer [128]byte
	if _, err := a.Read(ctx, buffer[:]); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read cancellation: %v", err)
	}
	b := wsDial(t, client)
	_ = wsPeer(t, peers)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Read(t.Context(), buffer[:]); err == nil {
		t.Fatal("client close left owned socket open")
	}
	if _, err := client.Dial(t.Context(), "/stream", "native"); !errors.Is(err, ErrClosed) {
		t.Fatal("closed client redialed")
	}
}
func TestUpgradeAuthenticationAndSubprotocol(t *testing.T) {
	client, server, _, _ := wsFixture(t, false, 128)
	if _, err := client.Dial(t.Context(), "/stream", "bad"); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("origin rejection: %v", err)
	}
	other, err := NewClient(Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig, MaxMessageBytes: 128, Subprotocol: "wrong.v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.Dial(t.Context(), "/stream", "native"); !errors.Is(err, ErrRejected) {
		t.Fatalf("subprotocol mismatch: %v", err)
	}
}
func TestProxyHTMLAndRedirectNeverExposeToken(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	edge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(403)
		_, _ = w.Write([]byte("echo secret-native-token"))
	}))
	defer edge.Close()
	client, err := NewClient(Config{Endpoint: edge.URL, TLSConfig: &tls.Config{InsecureSkipVerify: true}, MaxMessageBytes: 64})
	// httptest's HTTP client disables verification; use its certificate as a root instead.
	if !errors.Is(err, ErrEndpoint) {
		if client != nil {
			client.Close()
		}
		t.Fatal("disabled TLS verification accepted")
	}
	client = trustedTestClient(t, edge)
	for _, path := range []string{"/challenge", "/redirect"} {
		_, err = client.Dial(t.Context(), path, "secret-native-token")
		if err == nil || strings.Contains(err.Error(), "secret-native-token") {
			t.Fatal("edge body or bearer leaked")
		}
	}
	if targetCalls != 0 {
		t.Fatal("credentials followed a redirect")
	}
}
func TestEndpointPathAndCredentialValidation(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "https://example.com/path", "https://name:secret@example.com", "https://example.com?token=x"} {
		if _, err := NewClient(Config{Endpoint: endpoint, MaxMessageBytes: 64}); !errors.Is(err, ErrEndpoint) {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	client, _, _, _ := wsFixture(t, false, 128)
	for _, path := range []string{"https://other.example/x", "//other.example/x", "/x?token=secret", "/x#secret"} {
		if _, err := client.Dial(t.Context(), path, "native"); !errors.Is(err, ErrEndpoint) {
			t.Fatal("unsafe path accepted")
		}
	}
	if _, err := client.Dial(t.Context(), "/stream", "native\r\nInjected:x"); !errors.Is(err, ErrCredentials) {
		t.Fatal("header injection accepted")
	}
	if _, err := NewClient(Config{Endpoint: "https://example.com", MaxMessageBytes: 64, TLSConfig: &tls.Config{ServerName: "wrong.example"}}); !errors.Is(err, ErrEndpoint) {
		t.Fatal("TLS identity override accepted")
	}
}

func trustedTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := NewClient(Config{Endpoint: server.URL, TLSConfig: &tls.Config{RootCAs: roots}, MaxMessageBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestUpgradeUsesHTTP1EvenWhenHTTP2IsOffered(t *testing.T) {
	observed := make(chan *http.Request, 1)
	service := testkit.Service{StreamPath: "/stream", HandleStream: func(ctx context.Context, _ testkit.Call, send func([]byte) error) error {
		if err := send([]byte{1}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	handler := testkit.Handler(service)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r
		handler.ServeHTTP(w, r)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := NewClient(Config{Endpoint: server.URL, MaxMessageBytes: 64, TLSConfig: &tls.Config{RootCAs: roots, NextProtos: []string{"h2", "http/1.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := client.Dial(t.Context(), "/stream", "native")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var buffer [64]byte
	if _, err = conn.Read(t.Context(), buffer[:]); err != nil {
		t.Fatal(err)
	}
	request := <-observed
	if request.ProtoMajor != 1 || request.TLS.NegotiatedProtocol == "h2" {
		t.Fatal("upgrade did not remain HTTP/1.1")
	}
}
