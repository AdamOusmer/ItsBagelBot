// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package testkit supplies authenticated native WebSocket/HTTPS fixtures without
// exposing the vendor implementation to application tests or probe handlers.
package testkit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	vendor "github.com/coder/websocket"
)

type Call struct {
	Token, Path, ContentType string
	Headers                  map[string][]string
}
type Service struct {
	StreamPath      string
	Subprotocol     string
	MaxMessageBytes int
	// Authorize returns zero to allow, otherwise an HTTP rejection status before upgrade.
	Authorize    func(Call) int
	HandleStream func(context.Context, Call, func([]byte) error) error
	Posts        map[string]func(context.Context, Call, []byte) int
	// TextFrames exists only to verify binary-only client rejection in tests.
	TextFrames bool
}

// Handler is a standard native handler suitable for direct TLS fixtures or a
// Fiber HTTP adapter with verified Hijacker support. It forwards no domain policy.
func Handler(service Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := requestCall(r)
		if !authorize(w, call, service) {
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == service.StreamPath {
			serveStream(w, r, call, service)
			return
		}
		if r.Method == http.MethodPost {
			servePost(w, r, call, service)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}
func requestCall(request *http.Request) Call {
	call := Call{Path: request.URL.Path, ContentType: request.Header.Get("Content-Type"), Headers: request.Header.Clone()}
	if token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer "); ok {
		call.Token = token
	} else if token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "DPoP "); ok {
		call.Token = token
	}
	return call
}
func authorize(w http.ResponseWriter, call Call, service Service) bool {
	if service.Authorize != nil {
		if code := service.Authorize(call); code != 0 {
			w.WriteHeader(code)
			return false
		}
	}
	return true
}
func protocols(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
func serveStream(w http.ResponseWriter, r *http.Request, call Call, service Service) {
	conn, err := vendor.Accept(w, r, &vendor.AcceptOptions{CompressionMode: vendor.CompressionDisabled, Subprotocols: protocols(service.Subprotocol)})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	limit := service.MaxMessageBytes
	if limit <= 0 {
		limit = 64 << 10
	}
	conn.SetReadLimit(int64(limit))
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Reading control frames answers pings and observes close. Any application
	// message is rejected; the native app's socket direction is receive-only.
	monitor := conn.CloseRead(ctx)
	kind := vendor.MessageBinary
	if service.TextFrames {
		kind = vendor.MessageText
	}
	send := func(data []byte) error { return conn.Write(monitor, kind, data) }
	if service.HandleStream != nil {
		_ = service.HandleStream(monitor, call, send)
	}
}
func servePost(w http.ResponseWriter, r *http.Request, call Call, service Service) {
	handle := service.Posts[r.URL.Path]
	if handle == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	limit := service.MaxMessageBytes
	if limit <= 0 {
		limit = 64 << 10
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil || len(body) > limit {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	w.WriteHeader(handle(r.Context(), call, body))
}

type Server struct {
	Endpoint  string
	TLSConfig *tls.Config
	server    *httptest.Server
	count     atomic.Int64
}

func New(t testing.TB, service Service) *Server {
	t.Helper()
	server := httptest.NewUnstartedServer(Handler(service))
	result := &Server{server: server}
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			result.count.Add(1)
		}
	}
	server.StartTLS()
	certificate, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	result.Endpoint = server.URL
	result.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	t.Cleanup(result.Close)
	return result
}
func (s *Server) PhysicalConnections() int64 { return s.count.Load() }
func (s *Server) Close()                     { s.server.CloseClientConnections(); s.server.Close() }
