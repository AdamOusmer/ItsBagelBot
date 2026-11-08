// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testkit

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	shared "ItsBagelBot/pkg/grpc"
	"ItsBagelBot/pkg/grpc/internal/testpb"
)

func proxiedClient(t *testing.T, proxy *ReverseProxy) *shared.Client {
	t.Helper()
	client, err := shared.NewClient(shared.Config{Endpoint: proxy.Endpoint, TLSConfig: proxy.TLSConfig, MaxMessageBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func proxyObservation(t *testing.T, proxy *ReverseProxy) ProxyRequest {
	t.Helper()
	select {
	case observation := <-proxy.Requests():
		return observation
	case <-time.After(time.Second):
		t.Fatal("proxy did not observe request")
		return ProxyRequest{}
	}
}
func TestProxyPreservesStreamingUnaryAndTrailers(t *testing.T) {
	peers := make(chan *testPeer, 2)
	origin := New(t, fixtureService(peers))
	proxy := Proxy(t, origin)
	client := proxiedClient(t, proxy)
	stream := fixtureListen(t, client)
	peer := nextPeer(t, peers)
	peer.frames <- &testpb.Packet{Sequence: 1}
	fixtureReceive(t, stream, 1)
	var response testpb.Packet
	if err := client.Invoke(t.Context(), "/fixture.Test/Control", shared.Credentials{Token: "native"}, &testpb.Packet{Sequence: 20}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Sequence != 21 {
		t.Fatal("proxied unary response changed")
	}
	peer.frames <- &testpb.Packet{Sequence: 2}
	fixtureReceive(t, stream, 2)
	close(peer.end)
	if err := stream.Recv(t.Context(), &testpb.Packet{}); err != io.EOF {
		t.Fatalf("proxied terminal trailers: %v", err)
	}
	if !proxy.UpstreamHTTP2() {
		t.Fatal("origin leg did not negotiate TLS HTTP/2")
	}
	if proxy.PhysicalConnections() != 1 || origin.PhysicalConnections() != 1 {
		t.Fatal("proxy did not maintain independent shared HTTP/2 legs")
	}
	for i := 0; i < 2; i++ {
		assertProxyObservation(t, proxyObservation(t, proxy))
	}
	if len(peer.call.Headers[":authority"]) != 1 || peer.call.Headers[":authority"][0] == "" {
		t.Fatal("origin authority missing")
	}
}
func assertProxyObservation(t *testing.T, observation ProxyRequest) {
	t.Helper()
	if observation.HTTPMajor != 2 || observation.ALPN != "h2" {
		t.Fatal("downstream did not negotiate TLS HTTP/2")
	}
	if !strings.HasPrefix(observation.ContentType, "application/grpc") || observation.Authorization != "Bearer native" {
		t.Fatal("proxy changed gRPC content type or authorization")
	}
}
func TestProxyForwardsAuthenticationStatus(t *testing.T) {
	origin := New(t, fixtureService(make(chan *testPeer, 1)))
	proxy := Proxy(t, origin)
	client := proxiedClient(t, proxy)
	stream, err := client.Listen(t.Context(), "/fixture.Test/Listen", shared.Credentials{Token: "bad"}, &shared.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = stream.Recv(t.Context(), &testpb.Packet{}); !errors.Is(err, shared.ErrAuthentication) {
		t.Fatalf("proxied auth trailers: %v", err)
	}
}
func TestProxyReadCancellationDoesNotCloseUnaryChannel(t *testing.T) {
	peers := make(chan *testPeer, 1)
	origin := New(t, fixtureService(peers))
	proxy := Proxy(t, origin)
	client := proxiedClient(t, proxy)
	stream := fixtureListen(t, client)
	_ = nextPeer(t, peers)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := stream.Recv(ctx, &testpb.Packet{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("proxied cancellation: %v", err)
	}
	if err := client.Invoke(t.Context(), "/fixture.Test/Control", shared.Credentials{Token: "native"}, &testpb.Packet{}, &testpb.Packet{}); err != nil {
		t.Fatal("stream cancellation closed proxy channel:", err)
	}
}
