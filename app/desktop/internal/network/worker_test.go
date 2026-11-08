// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"
	"ItsBagelBot/pkg/websocket/testkit"
)

type workerFixture struct {
	worker   *worker
	packets  chan *wire.Frame
	sinkDone chan struct{}
	input    *io.PipeReader
}

type workerServerStream struct {
	messages chan *protocol.Message
	done     chan struct{}
}

func newWorkerFixture(t *testing.T, config shared.Config) *workerFixture {
	t.Helper()
	native, err := shared.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	input, output := io.Pipe()
	p := newPeer(t.Context(), strings.NewReader(""), output)
	f := &workerFixture{worker: &worker{peer: p, native: native, initialized: true}, packets: make(chan *wire.Frame, 16), sinkDone: make(chan struct{}), input: input}
	go f.collect()
	t.Cleanup(func() {
		f.worker.close()
		_ = input.Close()
		<-f.sinkDone
		for packet := range f.packets {
			packet.ReturnToVTPool()
		}
	})
	return f
}

func (f *workerFixture) collect() {
	defer close(f.sinkDone)
	defer close(f.packets)
	reader := frames{reader: f.input}
	for {
		message, err := reader.read()
		if err != nil {
			return
		}
		select {
		case f.packets <- message:
		case <-f.worker.peer.ctx.Done():
			message.ReturnToVTPool()
			return
		}
	}
}

func workerSocketFixture(t *testing.T) (*workerFixture, chan *workerServerStream) {
	t.Helper()
	streams := make(chan *workerServerStream, 2)
	server := testkit.New(t, testkit.Service{StreamPath: protocol.EventsPath, Subprotocol: protocol.Subprotocol, HandleStream: func(ctx context.Context, _ testkit.Call, send func([]byte) error) error {
		peer := &workerServerStream{messages: make(chan *protocol.Message, 4), done: make(chan struct{})}
		defer close(peer.done)
		select {
		case streams <- peer:
		case <-ctx.Done():
			return ctx.Err()
		}
		return serveWorkerSocket(ctx, peer, send)
	}})
	return newWorkerFixture(t, shared.Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig, MaxMessageBytes: protocol.MaxFrameBytes, Subprotocol: protocol.Subprotocol}), streams
}

func serveWorkerSocket(ctx context.Context, peer *workerServerStream, send func([]byte) error) error {
	for {
		select {
		case message := <-peer.messages:
			buffer := make([]byte, protocol.MaxFrameBytes)
			n, err := protobuf.MarshalTo(buffer, message)
			if err != nil {
				return err
			}
			if err = send(buffer[:n]); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (f *workerFixture) dial(id uint32) {
	r := wire.FrameFromVTPool()
	r.Kind = dialRequest
	r.Id = uint64(id)
	r.Stream = id
	r.Path = protocol.EventsPath
	r.Token = "native"
	r.Proof = "proof"
	if !f.worker.dispatch(r) {
		r.ReturnToVTPool()
	}
}

func nextWorkerPacket(t *testing.T, f *workerFixture) *wire.Frame {
	t.Helper()
	select {
	case message := <-f.packets:
		if message == nil {
			t.Fatal("worker output closed")
		}
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("worker output stalled")
		return nil
	}
}

func nextWorkerSocket(t *testing.T, streams chan *workerServerStream) *workerServerStream {
	t.Helper()
	select {
	case stream := <-streams:
		return stream
	case <-time.After(2 * time.Second):
		t.Fatal("worker upgrade missing")
		return nil
	}
}

func assertWorkerReply(t *testing.T, f *workerFixture, id uint32, code uint32) {
	t.Helper()
	reply := nextWorkerPacket(t, f)
	defer reply.ReturnToVTPool()
	if reply.Kind != response || reply.Id != uint64(id) || reply.Error != code {
		t.Fatalf("worker reply mismatch: kind=%d id=%d error=%d", reply.Kind, reply.Id, reply.Error)
	}
}

func workerKeepalive(t *testing.T, f *workerFixture, peer *workerServerStream, id uint32) {
	t.Helper()
	peer.messages <- &protocol.Message{Version: protocol.Version, Kind: protocol.Keepalive}
	message := nextWorkerPacket(t, f)
	defer message.ReturnToVTPool()
	if message.Kind != streamFrame || message.Stream != id || message.Message == nil || message.Message.Kind != protocol.Keepalive {
		t.Fatal("worker did not deliver a typed frame")
	}
}

func streamCountNow(w *worker) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := 0
	for _, stream := range w.streams {
		if stream != nil {
			count++
		}
	}
	return count
}

func awaitWorkerCount(t *testing.T, w *worker, count int) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for streamCountNow(w) != count {
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatal("owned worker stream did not drain")
		}
	}
}

func TestWorkerCloseWhileWaitingForAcknowledgementReleasesOwner(t *testing.T) {
	f, streams := workerSocketFixture(t)
	f.dial(1)
	assertWorkerReply(t, f, 1, noError)
	server := nextWorkerSocket(t, streams)
	workerKeepalive(t, f, server, 1)
	f.worker.stopStream(1)
	awaitWorkerCount(t, f.worker, 0)
	select {
	case <-server.done:
	case <-time.After(2 * time.Second):
		t.Fatal("close while ACK pending retained server socket")
	}
	if f.worker.peer.ctx.Err() != nil {
		t.Fatal("closing a stream cancelled the worker peer")
	}
}

func TestWorkerTwoStreamsRemainIndependentAndBounded(t *testing.T) {
	f, streams := workerSocketFixture(t)
	f.dial(1)
	assertWorkerReply(t, f, 1, noError)
	first := nextWorkerSocket(t, streams)
	workerKeepalive(t, f, first, 1)
	f.dial(2)
	assertWorkerReply(t, f, 2, noError)
	second := nextWorkerSocket(t, streams)
	workerKeepalive(t, f, second, 2)
	f.dial(3)
	assertWorkerReply(t, f, 3, unavailable)
	f.worker.stopStream(1)
	awaitWorkerCount(t, f.worker, 1)
	f.worker.acknowledge(2)
	workerKeepalive(t, f, second, 2)
	if f.worker.peer.ctx.Err() != nil {
		t.Fatal("closing primary interrupted candidate")
	}
}

func TestWorkerRejectsDuplicateStreamWithoutClosingExistingSocket(t *testing.T) {
	f, streams := workerSocketFixture(t)
	f.dial(1)
	assertWorkerReply(t, f, 1, noError)
	server := nextWorkerSocket(t, streams)
	workerKeepalive(t, f, server, 1)
	f.dial(1)
	assertWorkerReply(t, f, 1, unavailable)
	f.worker.acknowledge(1)
	workerKeepalive(t, f, server, 1)
}

func TestWorkerSuccessfulUpgradeOutlivesAttemptDeadline(t *testing.T) {
	f, streams := workerSocketFixture(t)
	f.dial(1)
	assertWorkerReply(t, f, 1, noError)
	server := nextWorkerSocket(t, streams)
	workerKeepalive(t, f, server, 1)
	f.worker.acknowledge(1)
	timer := time.NewTimer(5100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-server.done:
		t.Fatal("upgrade deadline cancelled a live stream")
	}
	workerKeepalive(t, f, server, 1)
}

func blockedUpgradeFixture(t *testing.T) (*workerFixture, chan struct{}, chan struct{}) {
	t.Helper()
	entered := make(chan struct{}, 1)
	cancelled := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
		close(cancelled)
	}))
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	config := shared.Config{Endpoint: server.URL, TLSConfig: server.Client().Transport.(*http.Transport).TLSClientConfig, MaxMessageBytes: protocol.MaxFrameBytes, Subprotocol: protocol.Subprotocol}
	return newWorkerFixture(t, config), entered, cancelled
}

func assertReservedUpgrade(t *testing.T, w *worker, id uint32) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, stream := range w.streams {
		if stream != nil && stream.id == id {
			if stream.socket != nil {
				t.Fatal("blocked upgrade already owns a socket")
			}
			return
		}
	}
	t.Fatal("upgrade was not reserved before asynchronous dial")
}

func TestWorkerCloseDuringUpgradeCancelsReservation(t *testing.T) {
	f, entered, cancelled := blockedUpgradeFixture(t)
	f.dial(1)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade did not begin")
	}
	assertReservedUpgrade(t, f.worker, 1)
	f.worker.stopStream(1)
	assertWorkerReply(t, f, 1, unavailable)
	awaitWorkerCount(t, f.worker, 0)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upgrade HTTP request was not cancelled")
	}
}

func TestWorkerRequestCancellationStopsUpgradeWithoutKillingPeer(t *testing.T) {
	f, entered, _ := blockedUpgradeFixture(t)
	f.dial(1)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade did not begin")
	}
	f.worker.handle(&wire.Frame{Kind: cancelRequest, Id: 1})
	assertWorkerReply(t, f, 1, unavailable)
	awaitWorkerCount(t, f.worker, 0)
	if f.worker.peer.ctx.Err() != nil {
		t.Fatal("normal RPC cancellation killed the worker")
	}
}

func TestWorkerPeerEOFJoinsActiveAndAcknowledgementWaiters(t *testing.T) {
	f, streams := workerSocketFixture(t)
	f.dial(1)
	assertWorkerReply(t, f, 1, noError)
	server := nextWorkerSocket(t, streams)
	workerKeepalive(t, f, server, 1)
	done := make(chan struct{})
	go func() { f.worker.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker EOF did not join socket and request owners")
	}
	if streamCountNow(f.worker) != 0 {
		t.Fatal("worker shutdown retained stream slots")
	}
}
