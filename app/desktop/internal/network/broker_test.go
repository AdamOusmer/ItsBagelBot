// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/network/wire"
)

type laneHarness struct {
	broker        *broker
	dialer        *virtualDialer
	parent, child *peer
}

func newLaneHarness(t *testing.T, dial func(context.Context, string) (net.Conn, error)) *laneHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	a, b := net.Pipe()
	parent, child := newPeer(ctx, a, a), newPeer(ctx, b, b)
	broker, files, err := newBroker(ctx, &parent.send, [2]string{"https://auth.example", "https://bridge.example"})
	if err != nil {
		t.Fatal(err)
	}
	broker.dial = dial
	parent.handle = func(m *wire.Frame) bool {
		switch m.Kind {
		case openLane:
			broker.open(m)
		case closeLane:
			broker.closeLane(m.Slot, m.Epoch)
		}
		return false
	}
	h := &laneHarness{broker, newVirtualDialer(child, files, broker.origins), parent, child}
	broker.start()
	go parent.run()
	go child.run()
	t.Cleanup(func() { cancel(); broker.close(); closeFiles(files); <-parent.done; <-child.done })
	return h
}
func openHarness(t *testing.T, h *laneHarness) *virtualConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := h.dialer.DialContext(ctx, "tcp", "auth.example:443")
	if err != nil {
		t.Fatal(err)
	}
	return conn.(*virtualConn)
}
func TestPublicAddressPolicy(t *testing.T) {
	denied := []string{"0.1.2.3", "10.0.0.1", "100.64.0.1", "100.127.255.254", "127.0.0.1", "169.254.169.254", "172.16.0.1", "192.0.0.9", "192.0.2.1", "192.88.99.1", "192.168.1.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::", "::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "64:ff9b::a9fe:a9fe", "64:ff9b:1::1", "100::1", "2001::1", "2001:2::1", "2001:db8::1", "2002:7f00:1::", "3fff::1", "fc00::1", "fe80::1", "ff02::1", "2606:4700::1%lo0"}
	for _, raw := range denied {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "104.16.0.1", "172.64.0.1", "2606:4700:4700::1111", "2001:4860:4860::8888", "::ffff:104.16.0.1"} {
		if !publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("blocked %s", raw)
		}
	}
}
func TestDialUsesValidatedLiteral(t *testing.T) {
	addresses := []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("2606:4700::1111"), netip.MustParseAddr("1.1.1.1")}
	var got []string
	_, err := dialResolved(context.Background(), addresses, "443", func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Error(network)
		}
		got = append(got, address)
		return nil, io.EOF
	})
	if !errors.Is(err, ErrWorker) || len(got) != 2 || got[0] != "[2606:4700::1111]:443" || got[1] != "1.1.1.1:443" {
		t.Fatalf("dialed %v, %v", got, err)
	}
}
func TestPublicOriginRejectsMalformedPorts(t *testing.T) {
	for _, raw := range []string{"https://auth.example:", "https://auth.example:0", "https://auth.example:65536", "https://auth.example?", "http://auth.example", "https://user@auth.example"} {
		if _, _, err := publicOrigin(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
func TestBrokerCanceledLateDialIsClosed(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	local, remote := net.Pipe()
	defer remote.Close()
	h := newLaneHarness(t, func(context.Context, string) (net.Conn, error) { close(started); <-release; return local, nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := h.dialer.DialContext(ctx, "tcp", "auth.example:443"); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	_ = remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("late socket survived: %v", err)
	}
}
func TestBrokerMaximumFourConnections(t *testing.T) {
	var active, maximum atomic.Int32
	h := newLaneHarness(t, func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		n := active.Add(1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		go func() { defer active.Add(-1); defer b.Close(); _, _ = io.Copy(io.Discard, b) }()
		return a, nil
	})
	var conns [laneCount]*virtualConn
	for i := range conns {
		conns[i] = openHarness(t, h)
	}
	if _, err := h.dialer.DialContext(context.Background(), "tcp", "auth.example:443"); !errors.Is(err, ErrWorker) {
		t.Fatal(err)
	}
	if maximum.Load() != laneCount {
		t.Fatal(maximum.Load())
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
}
func TestBrokerCloseUnblocksStoppedWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control, unused := net.Pipe()
	defer unused.Close()
	b, files, err := newBroker(ctx, &sender{writer: control}, [2]string{"https://auth.example"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFiles(files)
	b.dial = func(context.Context, string) (net.Conn, error) { return nil, io.EOF }
	b.start()
	b.open(&wire.Frame{Id: 1, Slot: 0, Epoch: 1})
	done := make(chan struct{})
	go func() { b.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broker shutdown blocked")
	}
}
