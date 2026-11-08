// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package testkit supplies real native TLS RPC fixtures to application tests.
// It is not an application server or a production serving API.
package testkit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shared "ItsBagelBot/pkg/grpc"
	vendor "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Call struct {
	Token   string
	Headers map[string][]string
}
type Factory func() shared.Message
type StreamMethod struct {
	Name       string
	NewRequest Factory
	Handle     func(context.Context, Call, shared.Message, func(shared.Message) error) error
}
type UnaryMethod struct {
	Name       string
	NewRequest Factory
	Handle     func(context.Context, Call, shared.Message) (shared.Message, error)
}
type Service struct {
	Name    string
	Streams []StreamMethod
	Methods []UnaryMethod
}

type countedListener struct {
	net.Listener
	count  atomic.Int64
	closed chan struct{}
	once   sync.Once
}

func (l *countedListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.count.Add(1)
	}
	return conn, err
}
func (l *countedListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

// Server exposes only the trust material needed by a native client, and test
// lifecycle operations. No vendor connection/server/descriptor type escapes.
type Server struct {
	Endpoint    string
	TLSConfig   *tls.Config
	rpc         *vendor.Server
	listener    *countedListener
	certificate tls.Certificate
	draining    sync.Once
	drained     chan struct{}
}

func New(t testing.TB, service Service) *Server {
	t.Helper()
	temporary := httptest.NewTLSServer(nil)
	certificate := temporary.TLS.Certificates[0]
	temporary.Close()
	return newAt(t, service, "127.0.0.1:0", certificate)
}
func newAt(t testing.TB, service Service, address string, certificate tls.Certificate) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	counted := &countedListener{Listener: listener, closed: make(chan struct{})}
	config := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
	rpc := vendor.NewServer(vendor.Creds(credentials.NewTLS(config)))
	rpc.RegisterService(describe(service), service)
	root, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	server := &Server{Endpoint: "https://" + listener.Addr().String(), TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, rpc: rpc, listener: counted, certificate: certificate, drained: make(chan struct{})}
	go rpc.Serve(counted)
	t.Cleanup(server.Stop)
	return server
}
func describe(service Service) *vendor.ServiceDesc {
	desc := &vendor.ServiceDesc{ServiceName: service.Name, HandlerType: (*any)(nil)}
	for _, method := range service.Streams {
		desc.Streams = append(desc.Streams, vendor.StreamDesc{StreamName: method.Name, ServerStreams: true, Handler: streamHandler(method)})
	}
	for _, method := range service.Methods {
		desc.Methods = append(desc.Methods, vendor.MethodDesc{MethodName: method.Name, Handler: unaryHandler(method)})
	}
	return desc
}
func streamHandler(method StreamMethod) vendor.StreamHandler {
	return func(_ any, stream vendor.ServerStream) error {
		request := method.NewRequest()
		if err := stream.RecvMsg(wireMessage(request)); err != nil {
			return err
		}
		var extra emptypb.Empty
		if err := stream.RecvMsg(&extra); err != io.EOF {
			return status.Error(codes.InvalidArgument, "fixture requires one request")
		}
		send := func(message shared.Message) error { return stream.SendMsg(wireMessage(message)) }
		return rpcError(method.Handle(stream.Context(), callFor(stream.Context()), request, send))
	}
}
func unaryHandler(method UnaryMethod) func(any, context.Context, func(any) error, vendor.UnaryServerInterceptor) (any, error) {
	return func(_ any, ctx context.Context, decode func(any) error, _ vendor.UnaryServerInterceptor) (any, error) {
		request := method.NewRequest()
		if err := decode(wireMessage(request)); err != nil {
			return nil, err
		}
		response, err := method.Handle(ctx, callFor(ctx), request)
		if err != nil {
			return nil, rpcError(err)
		}
		return wireMessage(response), nil
	}
}
func wireMessage(message shared.Message) any {
	if _, empty := message.(*shared.Empty); empty {
		return &emptypb.Empty{}
	}
	return message
}
func callFor(ctx context.Context) Call {
	headers, _ := metadata.FromIncomingContext(ctx)
	call := Call{Headers: headers.Copy()}
	values := headers.Get("authorization")
	if len(values) == 1 {
		if token, ok := strings.CutPrefix(values[0], "Bearer "); ok {
			call.Token = token
		}
	}
	return call
}
func rpcError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, shared.ErrAuthentication):
		return status.Error(codes.Unauthenticated, "fixture authentication rejected")
	case errors.Is(err, shared.ErrResourceExhausted), errors.Is(err, shared.ErrTooLarge):
		return status.Error(codes.ResourceExhausted, "fixture resource exhausted")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	default:
		return status.Error(codes.Internal, "fixture handler failed")
	}
}
func (s *Server) PhysicalConnections() int64 { return s.listener.count.Load() }
func (s *Server) Stop()                      { s.rpc.Stop() }
func (s *Server) beginDrain() {
	s.draining.Do(func() { go func() { s.rpc.GracefulStop(); close(s.drained) }() })
}
func (s *Server) Drain(ctx context.Context) error {
	s.beginDrain()
	select {
	case <-s.drained:
		return nil
	case <-ctx.Done():
		s.Stop()
		return ctx.Err()
	}
}

// Replace starts native graceful drain (including GOAWAY), then reopens the
// same authority with the same certificate. Existing RPCs remain on old until
// callers finish them; new RPCs can move to the replacement physical channel.
func (s *Server) Replace(t testing.TB, service Service) *Server {
	t.Helper()
	s.beginDrain()
	select {
	case <-s.listener.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("native server did not release listener")
	}
	return newAt(t, service, s.listener.Addr().String(), s.certificate)
}
