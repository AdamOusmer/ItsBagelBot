// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"
	"ItsBagelBot/pkg/websocket/testkit"
)

type bridgePeer struct {
	id     string
	frames chan *protocol.Message
	ended  chan struct{}
}
type bridgeBinding struct {
	session, migration, token string
	after                     uint64
}
type bridgeWire struct {
	token  string
	serial atomic.Uint64
	peers  chan *bridgePeer
	binds  chan bridgeBinding
}

func newBridgeWire() *bridgeWire {
	return &bridgeWire{token: "native-token", peers: make(chan *bridgePeer, 8), binds: make(chan bridgeBinding, 8)}
}
func (w *bridgeWire) service() testkit.Service {
	return testkit.Service{StreamPath: protocol.EventsPath, Subprotocol: protocol.Subprotocol, MaxMessageBytes: protocol.MaxFrameBytes, Authorize: func(call testkit.Call) int {
		if call.Token != w.token {
			return http.StatusUnauthorized
		}
		return 0
	}, HandleStream: w.listen, Posts: map[string]func(context.Context, testkit.Call, []byte) int{protocol.BindPath: w.bind, protocol.MigrationPath: w.migrate}}
}
func (w *bridgeWire) listen(ctx context.Context, call testkit.Call, send func([]byte) error) error {
	if call.Token != w.token {
		return shared.ErrAuthentication
	}
	if len(call.Headers["X-Bagel-After"]) != 0 || len(call.Headers["X-Bagel-Migration"]) != 0 {
		return errors.New("socket carried premature account binding")
	}
	peer := &bridgePeer{id: "session-" + strconv.FormatUint(w.serial.Add(1), 10), frames: make(chan *protocol.Message, 32), ended: make(chan struct{})}
	defer close(peer.ended)
	if err := sendBridgeMessage(send, &protocol.Message{Version: protocol.Version, Kind: protocol.SessionStarted, SessionId: peer.id, BindWithinMs: 5000}); err != nil {
		return err
	}
	select {
	case w.peers <- peer:
	case <-ctx.Done():
		return ctx.Err()
	}
	return serveBridgePeer(ctx, peer, send)
}
func sendBridgeMessage(send func([]byte) error, m *protocol.Message) error {
	size, err := protobuf.Size(m)
	if err != nil {
		return err
	}
	raw := make([]byte, size)
	if _, err = protobuf.MarshalTo(raw, m); err != nil {
		return err
	}
	return send(raw)
}
func serveBridgePeer(ctx context.Context, peer *bridgePeer, send func([]byte) error) error {
	for {
		select {
		case m := <-peer.frames:
			if err := sendBridgeMessage(send, m); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (w *bridgeWire) bind(ctx context.Context, call testkit.Call, raw []byte) int {
	if call.Token != w.token {
		return http.StatusUnauthorized
	}
	if call.ContentType != "application/protobuf" {
		return http.StatusUnsupportedMediaType
	}
	var req protocol.BindAccount
	if protobuf.Unmarshal(raw, &req) != nil || protocol.ValidateBind(&req) != nil {
		return http.StatusBadRequest
	}
	binding := bridgeBinding{session: req.SessionId, migration: req.Migration, token: call.Token, after: req.After}
	select {
	case w.binds <- binding:
		return http.StatusNoContent
	case <-ctx.Done():
		return http.StatusRequestTimeout
	}
}
func (w *bridgeWire) migrate(_ context.Context, call testkit.Call, raw []byte) int {
	if call.Token != w.token {
		return http.StatusUnauthorized
	}
	var req protocol.MigrationRequest
	if call.ContentType != "application/protobuf" || protobuf.Unmarshal(raw, &req) != nil || protocol.ValidateMigrationRequest(&req) != nil {
		return http.StatusBadRequest
	}
	return http.StatusNoContent
}
func nextBridgePeer(t *testing.T, wire *bridgeWire, migration string, after uint64) *bridgePeer {
	t.Helper()
	peer := nextUnboundBridgePeer(t, wire, migration, after)
	bridgeSend(t, peer, &protocol.Message{Version: protocol.Version, Kind: protocol.SessionBound, SessionId: peer.id})
	return peer
}
func nextUnboundBridgePeer(t *testing.T, wire *bridgeWire, migration string, after uint64) *bridgePeer {
	t.Helper()
	var peer *bridgePeer
	select {
	case peer = <-wire.peers:
	case <-time.After(3 * time.Second):
		t.Fatal("WSS session did not start")
	}
	select {
	case binding := <-wire.binds:
		if binding.session != peer.id || binding.migration != migration || binding.after != after || binding.token != wire.token {
			t.Fatal("wrong post-start protobuf binding")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native Bind did not follow session_started")
	}
	return peer
}
func bridgeSend(t *testing.T, peer *bridgePeer, m *protocol.Message) {
	t.Helper()
	select {
	case peer.frames <- m:
	case <-peer.ended:
		t.Fatal("stream ended before event")
	case <-time.After(time.Second):
		t.Fatal("event stalled")
	}
}
func bridgeEvent(seq uint64) *protocol.Message {
	return &protocol.Message{Version: protocol.Version, Kind: protocol.EventMessage, Event: &protocol.Event{Sequence: seq, Kind: protocol.EventKind_CHAT, Chat: &protocol.ChatEvent{ChannelId: "channel", UserId: "user", DisplayName: "name", Text: "event-" + strconv.FormatUint(seq, 10)}}}
}
func wireSessionConfig() session.Config {
	return session.Config{BackoffBase: 10 * time.Millisecond, BackoffMax: 50 * time.Millisecond, Jitter: func(time.Duration) time.Duration { return 0 }}
}
func startWireRuntime(t *testing.T, server *testkit.Server, token string) (*Runtime, <-chan uint64) {
	t.Helper()
	return startWireRuntimeAt(t, transport.Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig}, token, wireSessionConfig())
}
func startWireRuntimeAt(t *testing.T, config transport.Config, token string, timing session.Config) (*Runtime, <-chan uint64) {
	t.Helper()
	client, err := transport.NewDirectClient(config)
	if err != nil {
		t.Fatal(err)
	}
	applied := make(chan uint64, 32)
	consumer := runtimeConsumerFunc(func(event *protocol.Event) error {
		if event.Chat.Text != "event-"+strconv.FormatUint(event.Sequence, 10) {
			return errors.New("typed event changed")
		}
		applied <- event.Sequence
		return nil
	})
	r, err := New(Config{Endpoint: config.Endpoint, Tokens: runtimeTokenFunc(func(context.Context) (string, error) { return token, nil }), Consumer: consumer, Dial: client.Dial, Control: client, Session: timing})
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	go r.Run(ctx)
	t.Cleanup(func() {
		cancel()
		wait, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := r.Wait(wait); err != nil {
			t.Error(err)
		}
		client.Close()
	})
	if err = r.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, applied
}
func assertBridgeAlive(t *testing.T, peer *bridgePeer) {
	t.Helper()
	select {
	case <-peer.ended:
		t.Fatal("healthy primary closed before cutover")
	default:
	}
}
func finishWireHandoff(t *testing.T, r *Runtime, old, next *bridgePeer, ticket string, through uint64) {
	t.Helper()
	assertBridgeAlive(t, old)
	bridgeSend(t, next, bridgeEvent(through))
	bridgeSend(t, next, bridgeEvent(through+1))
	bridgeSend(t, next, &protocol.Message{Version: protocol.Version, Kind: protocol.HandoffReady, Migration: ticket, Through: through})
	bridgeSend(t, old, bridgeEvent(through))
	runtimeAwait(t, func() bool { return r.Status().State == session.Online && r.Status().Cursor == through+1 })
	select {
	case <-old.ended:
	case <-time.After(time.Second):
		t.Fatal("retired primary remained open")
	}
	assertBridgeAlive(t, next)
}
func assertBridgeSequence(t *testing.T, applied <-chan uint64, count uint64) {
	t.Helper()
	for want := uint64(1); want <= count; want++ {
		select {
		case got := <-applied:
			if got != want {
				t.Fatalf("got %d want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("handoff lost event")
		}
	}
	select {
	case got := <-applied:
		t.Fatalf("duplicate event %d", got)
	default:
	}
}
func TestNativeTypedHandoffUsesIndependentSockets(t *testing.T) {
	wire := newBridgeWire()
	server := testkit.New(t, wire.service())
	r, applied := startWireRuntime(t, server, wire.token)
	primary := nextBridgePeer(t, wire, "", 0)
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	bridgeSend(t, primary, bridgeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
	bridgeSend(t, primary, &protocol.Message{Version: protocol.Version, Kind: protocol.Reconnect, Migration: "rollout", WithinMs: 5000})
	pending := nextBridgePeer(t, wire, "rollout", 1)
	finishWireHandoff(t, r, primary, pending, "rollout", 2)
	bridgeSend(t, pending, &protocol.Message{Version: protocol.Version, Kind: protocol.Reconnect, Migration: "rebalance", WithinMs: 5000})
	last := nextBridgePeer(t, wire, "rebalance", 3)
	finishWireHandoff(t, r, pending, last, "rebalance", 4)
	assertBridgeSequence(t, applied, 5)
	if server.PhysicalConnections() != 4 {
		t.Fatalf("expected three WSS connections and one reused HTTPS control connection, got %d", server.PhysicalConnections())
	}
}
func TestNativeAuthenticationStopsWithoutBinding(t *testing.T) {
	wire := newBridgeWire()
	server := testkit.New(t, wire.service())
	r, _ := startWireRuntime(t, server, "wrong-account")
	runtimeAwait(t, func() bool { return r.Status().State == session.AuthenticationRequired })
	if len(wire.binds) != 0 || len(wire.peers) != 0 {
		t.Fatal("rejected account reached binding")
	}
}
func TestProxyFailureExhaustsRetriesWithoutSigningOut(t *testing.T) {
	var attempts atomic.Int32
	r := startRuntime(t, func(context.Context, transport.Request) (transport.Conn, error) {
		attempts.Add(1)
		return nil, shared.ErrProxyResponse
	}, runtimeConsumerFunc(func(*protocol.Event) error { return nil }))
	runtimeAwait(t, func() bool { return r.Status().State == session.Failed })
	if attempts.Load() != 3 || r.Status().Problem != session.ConnectionFailed {
		t.Fatal("proxy error escaped retry policy")
	}
}
func TestNativePendingUpgradeFailureKeepsPrimary(t *testing.T) {
	wire := newBridgeWire()
	service := wire.service()
	var upgrades atomic.Int32
	service.Authorize = func(call testkit.Call) int {
		if call.Token != wire.token {
			return http.StatusUnauthorized
		}
		if call.Path == protocol.EventsPath && upgrades.Add(1) > 1 {
			return http.StatusServiceUnavailable
		}
		return 0
	}
	server := testkit.New(t, service)
	r, applied := startWireRuntime(t, server, wire.token)
	primary := nextBridgePeer(t, wire, "", 0)
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	bridgeSend(t, primary, &protocol.Message{Version: 1, Kind: protocol.Reconnect, Migration: "move", WithinMs: 5000})
	runtimeAwait(t, func() bool { return r.Status().Problem == session.MigrationFailed })
	assertBridgeAlive(t, primary)
	bridgeSend(t, primary, bridgeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
	assertBridgeSequence(t, applied, 1)
	if upgrades.Load() != 6 {
		t.Fatalf("initial socket plus five migration attempts expected, got %d", upgrades.Load())
	}
}
func TestNativeEventsRequireSessionBound(t *testing.T) {
	wire := newBridgeWire()
	server := testkit.New(t, wire.service())
	timing := wireSessionConfig()
	timing.MaxAttempts = 1
	r, applied := startWireRuntimeAt(t, transport.Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig}, wire.token, timing)
	peer := nextUnboundBridgePeer(t, wire, "", 0)
	bridgeSend(t, peer, bridgeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().State == session.Failed })
	select {
	case <-applied:
		t.Fatal("event was applied before session_bound")
	default:
	}
}
