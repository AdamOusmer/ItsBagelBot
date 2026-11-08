// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"ItsBagelBot/app/desktop/internal/oauth"
	shared "ItsBagelBot/pkg/websocket"
)

type leaseProvider struct {
	dial           func(context.Context) (transport.Conn, error)
	post           func(context.Context) error
	dials          atomic.Int32
	globallyClosed atomic.Bool
}

func (p *leaseProvider) DialAuthorized(ctx context.Context, _ string, _ shared.Authorization) (transport.Conn, shared.Reply, error) {
	p.dials.Add(1)
	if p.dial != nil {
		conn, err := p.dial(ctx)
		return conn, shared.Reply{Status: 101}, err
	}
	return newLeaseConn(), shared.Reply{Status: 101}, nil
}
func (p *leaseProvider) PostAuthorized(ctx context.Context, _ string, _ shared.Authorization, _ string, _ []byte) (shared.Reply, error) {
	if p.post != nil {
		return shared.Reply{}, p.post(ctx)
	}
	return shared.Reply{Status: 204}, nil
}
func (p *leaseProvider) Close() error { p.globallyClosed.Store(true); return nil }
func (p *leaseProvider) Do(context.Context, oauth.NetworkRequest) (oauth.NetworkResponse, error) {
	if p.globallyClosed.Load() {
		return oauth.NetworkResponse{}, oauth.ErrUnavailable
	}
	return oauth.NetworkResponse{Status: 200, Token: oauth.TokenReply{Access: "global-oauth-still-available"}}, nil
}

type leaseConn struct {
	closes       atomic.Int32
	done         chan struct{}
	closeStarted chan struct{}
	closeRelease chan struct{}
	read         func(context.Context, *protocol.Message) error
}

func newLeaseConn() *leaseConn { return &leaseConn{done: make(chan struct{})} }
func (c *leaseConn) Recv(ctx context.Context, message *protocol.Message) error {
	if c.read != nil {
		return c.read(ctx, message)
	}
	select {
	case <-c.done:
		return shared.ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *leaseConn) Close() error {
	if c.closes.Add(1) == 1 {
		if c.closeStarted != nil {
			close(c.closeStarted)
			<-c.closeRelease
		}
		close(c.done)
	}
	return nil
}

func openLease(t *testing.T, port *bridgePort) transport.Conn {
	t.Helper()
	conn, _, err := port.DialAuthorized(t.Context(), protocol.EventsPath, shared.Authorization{Scheme: "DPoP", Token: "native", Proof: "proof"})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestBridgeLeaseClosePreservesGlobalOAuth(t *testing.T) {
	provider := &leaseProvider{}
	port := newBridgePort(t.Context(), provider)
	first, second := openLease(t, port), openLease(t, port)
	if err := port.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Do(t.Context(), oauth.NetworkRequest{}); err != nil || provider.globallyClosed.Load() {
		t.Fatal("account event scope closed global OAuth networking")
	}
	for _, conn := range []transport.Conn{first, second} {
		if err := conn.Recv(t.Context(), &protocol.Message{}); !errors.Is(err, shared.ErrClosed) {
			t.Fatal("account scope retained a live stream")
		}
	}
	if _, _, err := port.DialAuthorized(t.Context(), protocol.EventsPath, shared.Authorization{}); !errors.Is(err, shared.ErrClosed) {
		t.Fatal("closed account scope accepted another dial")
	}
}

func TestBridgeLeaseCloseCancelsUpgradeAndClosesLateConnection(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	late := newLeaseConn()
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	provider := &leaseProvider{dial: func(ctx context.Context) (transport.Conn, error) { entered <- ctx; <-release; return late, nil }}
	port := newBridgePort(t.Context(), provider)
	t.Cleanup(func() { _ = port.Close() })
	done := make(chan error, 1)
	go func() {
		_, _, err := port.DialAuthorized(t.Context(), protocol.EventsPath, shared.Authorization{})
		done <- err
	}()
	ctx := <-entered
	_ = port.Close()
	if ctx.Err() == nil {
		t.Fatal("scope close did not cancel pending upgrade")
	}
	once.Do(func() { close(release) })
	if err := <-done; !errors.Is(err, shared.ErrClosed) {
		t.Fatal("late upgrade escaped its closed account scope")
	}
	if late.closes.Load() != 1 {
		t.Fatal("late upgrade connection was not closed exactly once")
	}
}

func TestBridgeLeaseCloseCancelsControlsWithoutClosingProvider(t *testing.T) {
	entered := make(chan context.Context, 1)
	provider := &leaseProvider{post: func(ctx context.Context) error { entered <- ctx; <-ctx.Done(); return ctx.Err() }}
	port := newBridgePort(t.Context(), provider)
	t.Cleanup(func() { _ = port.Close() })
	done := make(chan error, 1)
	go func() {
		_, err := port.PostAuthorized(t.Context(), protocol.BindPath, shared.Authorization{}, "application/protobuf", nil)
		done <- err
	}()
	ctx := <-entered
	_ = port.Close()
	if ctx.Err() == nil {
		t.Fatal("scope close retained active control request")
	}
	if err := <-done; err == nil || provider.globallyClosed.Load() {
		t.Fatal("control cancellation closed application-owned provider")
	}
}

func TestBridgeLeaseRetainsClosingSlotUntilSocketReleased(t *testing.T) {
	closing := newLeaseConn()
	closing.closeStarted = make(chan struct{})
	closing.closeRelease = make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(closing.closeRelease) }) })
	provider := &leaseProvider{dial: func(context.Context) (transport.Conn, error) { return closing, nil }}
	port := newBridgePort(t.Context(), provider)
	first := openLease(t, port)
	provider.dial = func(context.Context) (transport.Conn, error) { return newLeaseConn(), nil }
	_ = openLease(t, port)
	done := make(chan struct{})
	go func() { _ = first.Close(); close(done) }()
	<-closing.closeStarted
	if _, _, err := port.DialAuthorized(t.Context(), protocol.EventsPath, shared.Authorization{}); err == nil {
		t.Fatal("closing socket released its bounded slot early")
	}
	once.Do(func() { close(closing.closeRelease) })
	<-done
	_ = openLease(t, port)
	_ = port.Close()
}

func TestBridgeLeaseControlConcurrencyIsBounded(t *testing.T) {
	entered := make(chan struct{}, 4)
	provider := &leaseProvider{post: func(ctx context.Context) error { entered <- struct{}{}; <-ctx.Done(); return ctx.Err() }}
	port := newBridgePort(t.Context(), provider)
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = port.PostAuthorized(t.Context(), protocol.BindPath, shared.Authorization{}, "application/protobuf", nil)
		}()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("scoped controls did not start")
		}
	}
	if _, err := port.PostAuthorized(t.Context(), protocol.BindPath, shared.Authorization{}, "application/protobuf", nil); err == nil {
		t.Fatal("account controls grew beyond fixed slots")
	}
	_ = port.Close()
	group.Wait()
}

func TestBridgeLeaseReceiveAddsNoSteadyStateAllocations(t *testing.T) {
	conn := newLeaseConn()
	conn.read = func(_ context.Context, message *protocol.Message) error {
		message.Version = protocol.Version
		message.Kind = protocol.Keepalive
		return nil
	}
	port := newBridgePort(t.Context(), &leaseProvider{dial: func(context.Context) (transport.Conn, error) { return conn, nil }})
	defer port.Close()
	stream := openLease(t, port)
	message := protocol.ServerMessageFromVTPool()
	defer message.ReturnToVTPool()
	ctx := t.Context()
	allocations := testing.AllocsPerRun(1000, func() {
		if err := stream.Recv(ctx, message); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("account scope receive allocated %g times", allocations)
	}
}
