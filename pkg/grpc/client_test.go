// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/grpc/internal/testpb"

	vendor "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const testListenMethod = "/bagel.shared.v1.Test/Listen"
const testInvokeMethod = "/bagel.shared.v1.Test/Control"

type countedListener struct {
	net.Listener
	count atomic.Int32
}

func (l *countedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.count.Add(1)
	}
	return conn, err
}

type testRPC interface {
	listen(vendor.ServerStream) error
	invoke(context.Context, *testpb.Packet) (*testpb.Packet, error)
}
type clientPeer struct {
	frames  chan *testpb.Packet
	finish  chan struct{}
	headers metadata.MD
	ended   chan struct{}
}
type clientTestServer struct {
	peers chan *clientPeer
	calls chan metadata.MD
}

func (s *clientTestServer) listen(stream vendor.ServerStream) error {
	var empty emptypb.Empty
	if err := stream.RecvMsg(&empty); err != nil {
		return err
	}
	if err := stream.RecvMsg(&empty); err != io.EOF {
		return status.Error(codes.InvalidArgument, "client stream remained open")
	}
	headers, _ := metadata.FromIncomingContext(stream.Context())
	if err := testAuthorization(headers); err != nil {
		return err
	}
	if len(headers.Get("authorization")) == 1 && headers.Get("authorization")[0] == "Bearer headers" {
		return stream.SendHeader(metadata.Pairs("x-overflow", strings.Repeat("x", 32<<10)))
	}
	peer := &clientPeer{frames: make(chan *testpb.Packet, 4), finish: make(chan struct{}), headers: headers, ended: make(chan struct{})}
	select {
	case s.peers <- peer:
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
	return serveClientPeer(stream, peer)
}
func testAuthorization(headers metadata.MD) error {
	token := headers.Get("authorization")
	if len(token) != 1 {
		return status.Error(codes.Unauthenticated, "credentials missing")
	}
	switch token[0] {
	case "Bearer bad":
		return status.Error(codes.Unauthenticated, "rejected")
	case "Bearer forbidden":
		return status.Error(codes.PermissionDenied, "rejected")
	}
	return nil
}
func serveClientPeer(stream vendor.ServerStream, peer *clientPeer) error {
	defer close(peer.ended)
	for {
		select {
		case frame := <-peer.frames:
			if err := stream.SendMsg(frame); err != nil {
				return err
			}
		case <-peer.finish:
			return nil
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}
func (s *clientTestServer) invoke(ctx context.Context, request *testpb.Packet) (*testpb.Packet, error) {
	headers, _ := metadata.FromIncomingContext(ctx)
	if err := testAuthorization(headers); err != nil {
		return nil, err
	}
	if len(headers.Get("authorization")) == 1 && headers.Get("authorization")[0] == "Bearer quota" {
		return nil, status.Error(codes.ResourceExhausted, "quota exceeded")
	}
	s.calls <- headers
	return &testpb.Packet{Sequence: request.Sequence + 1, Label: request.Label, Child: &testpb.Child{Value: "reply"}}, nil
}

var clientService = vendor.ServiceDesc{ServiceName: "bagel.shared.v1.Test", HandlerType: (*testRPC)(nil),
	Streams: []vendor.StreamDesc{{StreamName: "Listen", ServerStreams: true, Handler: func(server any, stream vendor.ServerStream) error { return server.(testRPC).listen(stream) }}},
	Methods: []vendor.MethodDesc{{MethodName: "Control", Handler: func(server any, ctx context.Context, decode func(any) error, _ vendor.UnaryServerInterceptor) (any, error) {
		var request testpb.Packet
		if err := decode(&request); err != nil {
			return nil, err
		}
		return server.(testRPC).invoke(ctx, &request)
	}}},
}

func clientFixture(t *testing.T, limit int) (*Client, *clientTestServer, *countedListener, Config) {
	t.Helper()
	temporary := httptest.NewTLSServer(nil)
	certificate := temporary.TLS.Certificates[0]
	temporary.Close()
	root, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	counted := &countedListener{Listener: listener}
	server := vendor.NewServer(vendor.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})))
	service := &clientTestServer{peers: make(chan *clientPeer, 4), calls: make(chan metadata.MD, 4)}
	server.RegisterService(&clientService, service)
	go server.Serve(counted)
	config := Config{Endpoint: "https://" + listener.Addr().String(), MaxMessageBytes: limit, TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}
	client, err := NewClient(config)
	if err != nil {
		server.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); server.Stop() })
	return client, service, counted, config
}
func nextClientPeer(t *testing.T, service *clientTestServer) *clientPeer {
	t.Helper()
	select {
	case peer := <-service.peers:
		return peer
	case <-time.After(time.Second):
		t.Fatal("stream did not reach native server")
		return nil
	}
}
func openClientStream(t *testing.T, client *Client, token string) (*Stream, *testpb.Packet) {
	t.Helper()
	stream, err := client.Listen(t.Context(), testListenMethod, Credentials{Token: token}, &Empty{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	return stream, &testpb.Packet{}
}
func TestClientMultiplexesNativeTLSAndUnary(t *testing.T) {
	client, service, counted, _ := clientFixture(t, 1024)
	old, _ := openClientStream(t, client, "account")
	_ = nextClientPeer(t, service)
	replacement, message := openClientStream(t, client, "account")
	peer := nextClientPeer(t, service)
	if err := client.Invoke(t.Context(), testInvokeMethod, Credentials{Token: "account"}, &testpb.Packet{Sequence: 9, Label: "payload"}, message); err != nil {
		t.Fatal(err)
	}
	if message.Sequence != 10 || message.Label != "payload" {
		t.Fatal("unary response mismatch")
	}
	_ = old.Close()
	peer.frames <- &testpb.Packet{Sequence: 11, Label: "live"}
	if err := replacement.Recv(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	if message.Sequence != 11 || message.Child != nil {
		t.Fatal("stream decode merged previous unary fields")
	}
	close(peer.finish)
	if err := replacement.Recv(t.Context(), message); err != io.EOF {
		t.Fatalf("EOF: %v", err)
	}
	if counted.count.Load() != 1 {
		t.Fatalf("overlap and unary used %d physical channels", counted.count.Load())
	}
}
func TestClientMetadataDoesNotInheritAnotherAccount(t *testing.T) {
	client, service, _, _ := clientFixture(t, 1024)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("authorization", "Bearer stale-account", "cookie", "private", "x-bagel-migration", "wrong"))
	var response testpb.Packet
	if err := client.Invoke(ctx, testInvokeMethod, Credentials{Token: "native-account"}, &testpb.Packet{}, &response); err != nil {
		t.Fatal(err)
	}
	headers := <-service.calls
	if values := headers.Get("authorization"); len(values) != 1 || values[0] != "Bearer native-account" {
		t.Fatal("authorization inherited another account")
	}
	if len(headers.Get("cookie")) != 0 || len(headers.Get("x-bagel-migration")) != 0 {
		t.Fatal("arbitrary application metadata leaked")
	}
}
func TestClientMapsAuthenticationAndLimits(t *testing.T) {
	client, service, _, _ := clientFixture(t, 64)
	for _, token := range []string{"bad", "forbidden"} {
		stream, message := openClientStream(t, client, token)
		if err := stream.Recv(t.Context(), message); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("auth error: %v", err)
		}
	}
	stream, message := openClientStream(t, client, "good")
	peer := nextClientPeer(t, service)
	peer.frames <- &testpb.Packet{Payload: make([]byte, 128)}
	if err := stream.Recv(t.Context(), message); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("receive bound: %v", err)
	}
	var reply testpb.Packet
	if err := client.Invoke(t.Context(), testInvokeMethod, Credentials{Token: "good"}, &testpb.Packet{Payload: make([]byte, 128)}, &reply); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("request bound: %v", err)
	}
}
func TestClientReadCancellationPreservesChannel(t *testing.T) {
	client, service, _, _ := clientFixture(t, 1024)
	stream, message := openClientStream(t, client, "good")
	_ = nextClientPeer(t, service)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := stream.Recv(ctx, message); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read deadline: %v", err)
	}
	if err := client.Invoke(t.Context(), testInvokeMethod, Credentials{Token: "good"}, &testpb.Packet{}, &testpb.Packet{}); err != nil {
		t.Fatal("stream cancellation closed shared channel:", err)
	}
}
func TestClientEndpointAndTLSConfiguration(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "https://example.com/path", "https://example.com?", "https://example.com?token=secret", "https://name:secret@example.com", "https://example.com#secret", "dns:///example.com", "https://example.com:0", "https://example.com:"} {
		if _, err := NewClient(Config{Endpoint: endpoint, MaxMessageBytes: 64}); !errors.Is(err, ErrEndpoint) {
			t.Fatalf("endpoint accepted: %v", err)
		}
	}
	for _, config := range []*tls.Config{{InsecureSkipVerify: true}, {ServerName: "wrong.example"}, {MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS12}} {
		if _, err := NewClient(Config{Endpoint: "https://example.com", MaxMessageBytes: 64, TLSConfig: config}); !errors.Is(err, ErrEndpoint) {
			t.Fatal("unsafe TLS configuration accepted")
		}
	}
}
func TestClientRejectsMissingAndInjectedCredentials(t *testing.T) {
	client, service, _, _ := clientFixture(t, 1024)
	for _, token := range []string{"", "secret\r\nInjected:yes", strings.Repeat("x", 8193)} {
		_, err := client.Listen(t.Context(), testListenMethod, Credentials{Token: token}, &Empty{})
		if !errors.Is(err, ErrCredentials) {
			t.Fatalf("credentials accepted: %v", err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("error disclosed credential value")
		}
	}
	if len(service.peers) != 0 {
		t.Fatal("invalid credentials reached account stream")
	}
}
func TestClientRejectsUntrustedTLSBeforeData(t *testing.T) {
	_, service, _, config := clientFixture(t, 1024)
	config.TLSConfig = nil
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	stream, err := client.Listen(ctx, testListenMethod, Credentials{Token: "good"}, &Empty{})
	if err == nil {
		defer stream.Close()
		err = stream.Recv(ctx, &testpb.Packet{})
	}
	if err == nil || len(service.peers) != 0 {
		t.Fatal("untrusted server received account data")
	}
}

func TestClientRejectsOversizedResponseHeaders(t *testing.T) {
	client, _, _, _ := clientFixture(t, 1024)
	stream, message := openClientStream(t, client, "headers")
	if err := stream.Recv(t.Context(), message); err == nil {
		t.Fatal("oversized metadata accepted")
	}
}
func TestClientResourceExhaustionIsNotAssumedToBeMessageSize(t *testing.T) {
	client, _, _, _ := clientFixture(t, 1024)
	err := client.Invoke(t.Context(), testInvokeMethod, Credentials{Token: "quota"}, &Empty{}, &Empty{})
	if !errors.Is(err, ErrResourceExhausted) || errors.Is(err, ErrTooLarge) {
		t.Fatalf("quota classification: %v", err)
	}
}
func TestClientCloseCancelsAllRPCs(t *testing.T) {
	client, service, _, _ := clientFixture(t, 1024)
	a, am := openClientStream(t, client, "account")
	ap := nextClientPeer(t, service)
	b, bm := openClientStream(t, client, "account")
	bp := nextClientPeer(t, service)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		stream  *Stream
		message Message
		peer    *clientPeer
	}{{a, am, ap}, {b, bm, bp}} {
		if err := pair.stream.Recv(t.Context(), pair.message); err == nil {
			t.Fatal("closed channel returned a message")
		}
		select {
		case <-pair.peer.ended:
		case <-time.After(time.Second):
			t.Fatal("server RPC outlived closed owner channel")
		}
	}
}

type panicSizeMessage struct{}

func (*panicSizeMessage) SizeVT() int                                { panic("private size panic") }
func (*panicSizeMessage) MarshalToSizedBufferVT([]byte) (int, error) { return 0, nil }
func (*panicSizeMessage) UnmarshalVT([]byte) error                   { return nil }
func (*panicSizeMessage) ResetVT()                                   {}
func TestClientPreflightContainsInvalidMessages(t *testing.T) {
	client, service, _, _ := clientFixture(t, 1024)
	var typedNil *testpb.Packet
	for _, request := range []Message{nil, typedNil, &panicSizeMessage{}} {
		err := client.Invoke(t.Context(), testInvokeMethod, Credentials{Token: "account"}, request, &Empty{})
		if !errors.Is(err, ErrUnsupportedMessage) && !errors.Is(err, ErrInvalidMessage) {
			t.Fatalf("invalid request escaped preflight: %v", err)
		}
	}
	if err := client.Invoke(t.Context(), testInvokeMethod, Credentials{Token: "account"}, &Empty{}, nil); !errors.Is(err, ErrUnsupportedMessage) {
		t.Fatalf("invalid response accepted: %v", err)
	}
	if len(service.calls) != 0 {
		t.Fatal("invalid message reached native server")
	}
}
