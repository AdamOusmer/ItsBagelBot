// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/websocket/testkit"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

type tunnelProbeConfig struct{ host, token, certificate, key, proofJKT string }

func readTunnelProbeConfig() (tunnelProbeConfig, error) {
	config := tunnelProbeConfig{host: os.Getenv("BAGEL_TUNNEL_HOST"), certificate: os.Getenv("BAGEL_TUNNEL_TLS_CERT_FILE"), key: os.Getenv("BAGEL_TUNNEL_TLS_KEY_FILE")}
	raw, err := os.ReadFile(os.Getenv("BAGEL_TUNNEL_TOKEN_FILE"))
	if err != nil {
		return config, errors.New("probe token file unavailable")
	}
	config.token = strings.TrimSpace(string(raw))
	config.proofJKT = os.Getenv("BAGEL_TUNNEL_DPOP_JKT")
	if !validProbeConfig(config) {
		return config, errors.New("invalid probe configuration")
	}
	return config, nil
}

func validProbeConfig(config tunnelProbeConfig) bool {
	if config.host == "" || strings.ContainsAny(config.host, "/\\\r\n\t ") {
		return false
	}
	if config.certificate == "" || config.key == "" {
		return false
	}
	if len(config.token) < 32 || len(config.token) > 8192 {
		return false
	}
	for _, c := range []byte(config.token) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func tunnelProbeApp(ctx context.Context, host string, wire *bridgeWire) *fiber.App {
	return tunnelProbeAppWithProof(ctx, host, wire, "")
}
func tunnelProbeAppWithProof(ctx context.Context, host string, wire *bridgeWire, jkt string) *fiber.App {
	app := fiber.New(fiber.Config{ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, BodyLimit: protocol.MaxFrameBytes, Concurrency: 64, JSONEncoder: codec.Marshal, JSONDecoder: codec.Unmarshal})
	expected := []byte("Bearer " + wire.token)
	guard := newProbeProofGuard(host, wire.token, jkt)
	app.Use(func(c fiber.Ctx) error {
		if c.Path() == "/healthz" && c.Method() == fiber.MethodGet {
			return c.SendStatus(fiber.StatusNoContent)
		}
		if string(c.Request().Host()) != host {
			return c.Status(fiber.StatusForbidden).Send(nil)
		}
		if jkt != "" {
			return guard.authorize(c)
		}
		if subtle.ConstantTimeCompare([]byte(c.Get("Authorization")), expected) != 1 {
			return c.Status(fiber.StatusUnauthorized).Send(nil)
		}
		return c.Next()
	})
	service := wire.service()
	handle := service.HandleStream
	service.HandleStream = func(parent context.Context, call testkit.Call, send func([]byte) error) error {
		child, cancel := context.WithCancel(parent)
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		defer cancel()
		return handle(child, call, send)
	}
	app.Use(adaptor.HTTPHandler(probeUpgradeHandler(testkit.Handler(service))))
	return app
}

// This opt-in fixture is compiled into a standalone test binary, not the
// desktop release. It is served only in a temporary, internally routed probe pod.
func TestTunnelProbeServer(t *testing.T) {
	if os.Getenv("BAGEL_TUNNEL_SERVER") != "1" {
		t.Skip("opt-in tunnel probe server")
	}
	config, err := readTunnelProbeConfig()
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(config.certificate, config.key)
	if err != nil {
		t.Fatal("probe TLS files unavailable or invalid")
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 10*time.Minute)
	defer cancel()
	wire := newBridgeWire()
	wire.token = config.token
	app := tunnelProbeAppWithProof(ctx, config.host, wire, config.proofJKT)
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		t.Fatal("probe listener unavailable")
	}
	secure := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
	done := make(chan error, 1)
	go func() { done <- app.Listener(secure, fiber.ListenConfig{DisableStartupMessage: true}) }()
	defer shutdownProbeApp(app, secure)
	t.Log("probe listener started")
	if err = coordinateTunnelProbe(ctx, wire, t.Log); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-done:
		t.Fatal("probe listener ended unexpectedly")
	}
}

func shutdownProbeApp(app *fiber.App, listener net.Listener) {
	_ = listener.Close()
	_ = app.ShutdownWithTimeout(3 * time.Second)
}

func coordinateTunnelProbe(ctx context.Context, wire *bridgeWire, log func(...any)) error {
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
	primary, err = probeMove(ctx, wire, primary, "move-1", 2, 3)
	if err != nil {
		return err
	}
	if err = probeSend(ctx, primary, bridgeEvent(5)); err != nil {
		return err
	}
	log("first migration completed; idle soak started")
	if err = probeIdle(ctx, primary, 150*time.Second, false); err != nil {
		return err
	}
	log("idle soak completed after 150 seconds")
	primary, err = probeMove(ctx, wire, primary, "move-2", 5, 6)
	if err != nil {
		return err
	}
	if err = probeSend(ctx, primary, bridgeEvent(8)); err != nil {
		return err
	}
	log("probe sequence completed; two migrations; cursor 8")
	return probeIdle(ctx, primary, 10*time.Minute, true)
}

func probeAttach(ctx context.Context, wire *bridgeWire, migration string, after uint64) (*bridgePeer, error) {
	wait := 30 * time.Second
	// Initial deployment may finish before the temporary Tunnel route is ready.
	if migration == "" {
		wait = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var peer *bridgePeer
	select {
	case peer = <-wire.peers:
	case <-ctx.Done():
		return nil, errors.New("probe session did not start")
	}
	select {
	case bind := <-wire.binds:
		if bind.session != peer.id || bind.migration != migration || bind.after != after || subtle.ConstantTimeCompare([]byte(bind.token), []byte(wire.token)) != 1 {
			return nil, errors.New("probe account binding mismatch")
		}
	case <-ctx.Done():
		return nil, errors.New("probe account binding did not arrive")
	}
	err := probeSend(ctx, peer, &protocol.Message{Version: protocol.Version, Kind: protocol.SessionBound, SessionId: peer.id})
	return peer, err
}

func probeMove(ctx context.Context, wire *bridgeWire, primary *bridgePeer, migration string, after, through uint64) (*bridgePeer, error) {
	reconnect := &protocol.Message{Version: protocol.Version, Kind: protocol.Reconnect, Migration: migration, WithinMs: 10000}
	if err := probeSend(ctx, primary, reconnect); err != nil {
		return nil, err
	}
	candidate, err := probeAttach(ctx, wire, migration, after)
	if err != nil {
		return nil, err
	}
	if err = probeSend(ctx, candidate, bridgeEvent(through+1)); err != nil {
		return nil, err
	}
	ready := &protocol.Message{Version: protocol.Version, Kind: protocol.HandoffReady, Migration: migration, Through: through}
	if err = probeSend(ctx, candidate, ready); err != nil {
		return nil, err
	}
	if err = probeSend(ctx, primary, bridgeEvent(through)); err != nil {
		return nil, err
	}
	if err = probeClosed(ctx, primary); err != nil {
		return nil, err
	}
	return candidate, nil
}

func probeSend(ctx context.Context, peer *bridgePeer, message *protocol.Message) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case peer.frames <- message:
		return nil
	case <-peer.ended:
		return errors.New("probe source ended early")
	case <-timer.C:
		return errors.New("probe source queue stalled")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func probeClosed(ctx context.Context, peer *bridgePeer) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-peer.ended:
		return nil
	case <-timer.C:
		return errors.New("probe prior source did not close after handoff")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func probeIdle(ctx context.Context, peer *bridgePeer, duration time.Duration, allowClose bool) error {
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := probeSend(ctx, peer, &protocol.Message{Version: protocol.Version, Kind: protocol.Keepalive}); err != nil {
				return err
			}
		case <-deadline.C:
			return nil
		case <-peer.ended:
			if !allowClose {
				return errors.New("probe source closed during idle soak")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
