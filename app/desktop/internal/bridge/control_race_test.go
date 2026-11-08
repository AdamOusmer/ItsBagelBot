// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/pkg/websocket/testkit"
)

// Origin acceptance and its authoritative socket acknowledgement can precede a
// failed/lost HTTPS control response. The held response reproduces that ordering
// over real TLS, rather than synthesizing a supervisor notification directly.
type lateControlFixture struct {
	wire      *bridgeWire
	service   testkit.Service
	release   chan struct{}
	once      sync.Once
	responses chan struct{}
	requests  chan struct{}
	count     atomic.Int32
	hold      int32
	status    int
}

func newLateControlFixture(t *testing.T, hold int32, status int) *lateControlFixture {
	t.Helper()
	f := &lateControlFixture{wire: newBridgeWire(), release: make(chan struct{}), responses: make(chan struct{}, 1), requests: make(chan struct{}, 2), hold: hold, status: status}
	f.service = f.wire.service()
	f.service.Posts[protocol.BindPath] = f.bind
	f.service.Posts[protocol.MigrationPath] = f.migrate
	t.Cleanup(f.unblock)
	return f
}
func (f *lateControlFixture) unblock() { f.once.Do(func() { close(f.release) }) }
func (f *lateControlFixture) bind(ctx context.Context, call testkit.Call, raw []byte) int {
	status := f.wire.bind(ctx, call, raw)
	if status != http.StatusNoContent || f.count.Add(1) != f.hold {
		return status
	}
	select {
	case <-f.release:
		f.responses <- struct{}{}
		return f.status
	case <-ctx.Done():
		return http.StatusRequestTimeout
	}
}
func (f *lateControlFixture) migrate(ctx context.Context, call testkit.Call, raw []byte) int {
	status := f.wire.migrate(ctx, call, raw)
	if status == http.StatusNoContent {
		select {
		case f.requests <- struct{}{}:
		case <-ctx.Done():
			return http.StatusRequestTimeout
		}
	}
	return status
}
func assertFutureControlServed(t *testing.T, r *Runtime, f *lateControlFixture, peer *bridgePeer) {
	t.Helper()
	bridgeSend(t, peer, &protocol.Message{Version: 1, Kind: protocol.Reconnect, Migration: "invalid", WithinMs: 5000, Target: "https://untrusted.example"})
	runtimeAwait(t, func() bool { return r.Status().Problem == session.MigrationFailed })
	if err := r.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.requests:
	case <-time.After(time.Second):
		t.Fatal("late control failure killed controller for future requests")
	}
	assertBridgeAlive(t, peer)
}
func TestLateHTTPSBindFailureDoesNotCloseBoundPrimary(t *testing.T) {
	f := newLateControlFixture(t, 1, http.StatusServiceUnavailable)
	server := testkit.New(t, f.service)
	r, _ := startWireRuntime(t, server, f.wire.token)
	peer := nextBridgePeer(t, f.wire, "", 0)
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	f.unblock()
	assertFutureControlServed(t, r, f, peer)
	bridgeSend(t, peer, bridgeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
}
func TestLateHTTPSBindFailureDoesNotClosePromotedCandidate(t *testing.T) {
	f := newLateControlFixture(t, 2, http.StatusServiceUnavailable)
	server := testkit.New(t, f.service)
	r, _ := startWireRuntime(t, server, f.wire.token)
	primary := nextBridgePeer(t, f.wire, "", 0)
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	bridgeSend(t, primary, bridgeEvent(1))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 1 })
	bridgeSend(t, primary, &protocol.Message{Version: 1, Kind: protocol.Reconnect, Migration: "move", WithinMs: 5000})
	next := nextBridgePeer(t, f.wire, "move", 1)
	finishWireHandoff(t, r, primary, next, "move", 2)
	f.unblock()
	assertFutureControlServed(t, r, f, next)
	bridgeSend(t, next, bridgeEvent(4))
	runtimeAwait(t, func() bool { return r.Status().Cursor == 4 })
}
func TestLateHTTPSAuthenticationFailureStillRequiresSignIn(t *testing.T) {
	f := newLateControlFixture(t, 1, http.StatusUnauthorized)
	server := testkit.New(t, f.service)
	r, _ := startWireRuntime(t, server, f.wire.token)
	_ = nextBridgePeer(t, f.wire, "", 0)
	runtimeAwait(t, func() bool { return r.Status().State == session.Online })
	f.unblock()
	runtimeAwait(t, func() bool { return r.Status().State == session.AuthenticationRequired })
}
