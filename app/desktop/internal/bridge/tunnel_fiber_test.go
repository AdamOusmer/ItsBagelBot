// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"

	"github.com/gofiber/fiber/v3"
)

func fiberProbeFixture(t *testing.T) (transport.Config, *bridgeWire) {
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
	ctx, cancel := context.WithCancel(t.Context())
	wire := newBridgeWire()
	app := tunnelProbeApp(ctx, listener.Addr().String(), wire)
	secure := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.Listener(secure, fiber.ListenConfig{DisableStartupMessage: true, BeforeServeFunc: func(*fiber.App) error { close(ready); return nil }})
	}()
	t.Cleanup(func() { cancel(); shutdownProbeApp(app, secure) })
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Fiber TLS listener: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Fiber TLS listener stalled")
	}
	return transport.Config{Endpoint: "https://" + listener.Addr().String(), TLSConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, wire
}

func TestFiberTLSWebsocketHandoff(t *testing.T) {
	config, wire := fiberProbeFixture(t)
	runtime, applied := startWireRuntimeAt(t, config, wire.token, wireSessionConfig())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- firstFiberHandoff(ctx, wire) }()
	done = awaitFiberEvents(t, ctx, runtime, applied, done)
	runtimeAwait(t, func() bool { return runtime.Status().State == session.Online && runtime.Status().Cursor == 5 })
	if done != nil {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("Fiber probe handoff coordinator stalled")
		}
	}
}

func awaitFiberEvents(t *testing.T, ctx context.Context, runtime *Runtime, applied <-chan uint64, done chan error) chan error {
	t.Helper()
	for want := uint64(1); want <= 5; {
		select {
		case got := <-applied:
			if got != want {
				t.Fatalf("Fiber event %d want %d", got, want)
			}
			want++
		case err := <-done:
			if err != nil {
				t.Fatalf("Fiber coordinator ended: %v", err)
			}
			done = nil
		case <-ctx.Done():
			t.Fatalf("Fiber event timeout, status %+v", runtime.Status())
		}
	}

	return done
}

func firstFiberHandoff(ctx context.Context, wire *bridgeWire) error {
	primary, err := probeAttach(ctx, wire, "", 0)
	if err != nil {
		return err
	}
	if err = probeSend(ctx, primary, bridgeEvent(1)); err != nil {
		return err
	}
	if err = probeSend(ctx, primary, bridgeEvent(2)); err != nil {
		return err
	}
	next, err := probeMove(ctx, wire, primary, "move-1", 2, 3)
	if err != nil {
		return err
	}
	return probeSend(ctx, next, bridgeEvent(5))
}

func TestFiberProbeRejectsUnauthenticatedAndWrongHost(t *testing.T) {
	config, _ := fiberProbeFixture(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config.TLSConfig.Clone()}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, entry := range []struct {
		path, host string
		status     int
	}{
		{"/healthz", "", http.StatusNoContent},
		{"/bridge/v1/events", "", http.StatusUnauthorized},
		{"/bridge/v1/bind", "", http.StatusUnauthorized},
		{"/bridge/v1/events", "other.example", http.StatusForbidden},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, config.Endpoint+entry.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if entry.host != "" {
			request.Host = entry.host
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != entry.status {
			t.Fatalf("%s returned status %d", entry.path, response.StatusCode)
		}
		if response.ContentLength > 0 {
			t.Fatal("probe rejection disclosed a response body")
		}
	}
}

func TestFiberTLSBareSocket(t *testing.T) {
	config, wire := fiberProbeFixture(t)
	client, err := shared.NewClient(shared.Config{Endpoint: config.Endpoint, TLSConfig: config.TLSConfig, MaxMessageBytes: protocol.MaxFrameBytes, Subprotocol: protocol.Subprotocol})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	socket, err := client.Dial(ctx, protocol.EventsPath, wire.token)
	if err != nil {
		t.Fatalf("Fiber upgrade: %v", err)
	}
	defer socket.Close()
	var buffer [64 << 10]byte
	n, err := socket.Read(ctx, buffer[:])
	if err != nil {
		t.Fatalf("Fiber welcome receive: %v bytes=%d", err, n)
	}
	var message protocol.Message
	if err = protobuf.Unmarshal(buffer[:n], &message); err != nil {
		t.Fatal(err)
	}
	defer message.ResetVT()
	if message.Kind != protocol.SessionStarted {
		t.Fatalf("kind %v", message.Kind)
	}
}
