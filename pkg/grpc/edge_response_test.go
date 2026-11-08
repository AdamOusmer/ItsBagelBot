// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/grpc/internal/testpb"
	vendor "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func edgeFixture(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	edge := httptest.NewUnstartedServer(handler)
	edge.EnableHTTP2 = true
	edge.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	edge.StartTLS()
	t.Cleanup(func() { edge.CloseClientConnections(); edge.Close() })
	roots := x509.NewCertPool()
	roots.AddCert(edge.Certificate())
	client, err := NewClient(Config{Endpoint: edge.URL, MaxMessageBytes: 1024, TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func streamFailure(ctx context.Context, client *Client) error {
	stream, err := client.Listen(ctx, testListenMethod, Credentials{Token: "disposable-edge-token"}, &Empty{})
	if err != nil {
		return err
	}
	defer stream.Close()
	return stream.Recv(ctx, &testpb.Packet{})
}

func TestHTTPChallengeIsNotAccountRejection(t *testing.T) {
	requests := make(chan string, 4)
	client := edgeFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("fixture did not negotiate HTTP/2")
		}
		requests <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html>private challenge-body marker</html>"))
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := streamFailure(ctx, client); err != ErrProxyResponse {
		t.Fatalf("challenge error: %v", err)
	}
	var reply testpb.Packet
	err := client.Invoke(ctx, testInvokeMethod, Credentials{Token: "disposable-edge-token"}, &testpb.Packet{}, &reply)
	if err != ErrProxyResponse || errors.Is(err, ErrAuthentication) {
		t.Fatalf("challenge unary error: %v", err)
	}
	for range 2 {
		if got := <-requests; got != "Bearer disposable-edge-token" {
			t.Fatalf("source metadata: %q", got)
		}
	}
}

func TestHTTPRedirectIsNotFollowed(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Add(1); w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	client := edgeFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Location", target.URL+"/private-login")
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("sensitive login response"))
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := streamFailure(ctx, client); err != ErrProxyResponse {
		t.Fatalf("redirect error: %v", err)
	}
	var reply testpb.Packet
	if err := client.Invoke(ctx, testInvokeMethod, Credentials{Token: "disposable-edge-token"}, &testpb.Packet{}, &reply); err != ErrProxyResponse {
		t.Fatalf("redirect unary error: %v", err)
	}
	if followed.Load() != 0 {
		t.Fatal("credentials were forwarded to redirected authority")
	}
}

func TestTrailerOnlyNativeAuthenticationProof(t *testing.T) {
	client, _, _, _ := clientFixture(t, 1024)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for _, token := range []string{"bad", "forbidden"} {
		stream, err := client.Listen(ctx, testListenMethod, Credentials{Token: token}, &Empty{})
		if err == nil {
			defer stream.Close()
			err = stream.Recv(ctx, &testpb.Packet{})
		}
		if err != ErrAuthentication {
			t.Fatalf("native trailer-only stream auth: %v", err)
		}
		var reply testpb.Packet
		if err = client.Invoke(ctx, testInvokeMethod, Credentials{Token: token}, &testpb.Packet{}, &reply); err != ErrAuthentication {
			t.Fatalf("native trailer-only unary auth: %v", err)
		}
	}
}

type headerAuthPeer struct{}

func (headerAuthPeer) listen(stream vendor.ServerStream) error {
	if err := stream.SendHeader(metadata.Pairs("x-proof", "initial-header")); err != nil {
		return err
	}
	return status.Error(codes.PermissionDenied, "private RPC rejection reason")
}
func (headerAuthPeer) invoke(ctx context.Context, _ *testpb.Packet) (*testpb.Packet, error) {
	if err := vendor.SendHeader(ctx, metadata.Pairs("x-proof", "initial-header")); err != nil {
		return nil, err
	}
	return nil, status.Error(codes.Unauthenticated, "private RPC rejection reason")
}

func initialHeaderAuthFixture(t *testing.T) *Client {
	t.Helper()
	seed := httptest.NewTLSServer(nil)
	certificate := seed.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(seed.Certificate())
	seed.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peer := vendor.NewServer(vendor.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})))
	peer.RegisterService(&clientService, headerAuthPeer{})
	go peer.Serve(listener)
	t.Cleanup(peer.Stop)
	client, err := NewClient(Config{Endpoint: "https://" + listener.Addr().String(), MaxMessageBytes: 1024, TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestInitialHeaderNativeAuthenticationProof(t *testing.T) {
	client := initialHeaderAuthFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := streamFailure(ctx, client); err != ErrAuthentication {
		t.Fatalf("initial header stream auth: %v", err)
	}
	var reply testpb.Packet
	if err := client.Invoke(ctx, testInvokeMethod, Credentials{Token: "disposable-edge-token"}, &testpb.Packet{}, &reply); err != ErrAuthentication {
		t.Fatalf("initial header unary auth: %v", err)
	}
}

func TestRPCContentTypeProof(t *testing.T) {
	for _, value := range []string{"application/grpc", "application/grpc+proto", "application/grpc; charset=utf-8"} {
		if !hasRPCContentType(metadata.Pairs("content-type", value)) {
			t.Fatalf("rejected RPC content type %q", value)
		}
	}
	for _, value := range []string{"", "text/html", "application/grpc-web", "application/grpcish"} {
		if hasRPCContentType(metadata.Pairs("content-type", value)) {
			t.Fatalf("accepted non-RPC content type %q", value)
		}
	}
	if err := normalize(status.Error(codes.PermissionDenied, "private reason")); err != ErrProxyResponse {
		t.Fatal("missing proof was treated as account rejection")
	}
}
