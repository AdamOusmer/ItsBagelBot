// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/network/wire"
)

func TestMalformedWorkerHelloCannotTriggerBootstrap(t *testing.T) {
	for _, message := range []*wire.Frame{{Kind: hello}, {Kind: response, Isolated: true}} {
		var output bytes.Buffer
		p := newPeer(t.Context(), strings.NewReader(""), &output)
		client := &Client{peer: p, ready: make(chan *wire.Frame, 1)}
		hello := wire.FrameFromVTPool()
		hello.Kind = message.Kind
		hello.Isolated = message.Isolated
		client.ready <- hello
		if err := client.initialize(t.Context(), Config{AuthEndpoint: "https://auth.example"}); !errors.Is(err, ErrWorker) {
			t.Fatal("malformed isolation handshake accepted")
		}
		if output.Len() != 0 {
			t.Fatal("bootstrap data was sent before isolation handshake validation")
		}
	}
}

func parentClientFixture(t *testing.T) *Client {
	t.Helper()
	p := newPeer(t.Context(), strings.NewReader(""), io.Discard)
	client := &Client{peer: p, cancel: p.cancel, ready: make(chan *wire.Frame, 1)}
	t.Cleanup(func() {
		_ = client.Close()
		for {
			select {
			case frame := <-client.ready:
				frame.ReturnToVTPool()
			default:
				return
			}
		}
	})
	return client
}

func TestClientCloseReleasesPendingHandshakeLease(t *testing.T) {
	client := parentClientFixture(t)
	first := wire.FrameFromVTPool()
	first.Kind = hello
	first.Isolated = true
	if !client.handle(first) {
		first.ReturnToVTPool()
		t.Fatal("valid first hello was not retained")
	}
	duplicate := wire.FrameFromVTPool()
	duplicate.Kind = hello
	duplicate.Isolated = true
	if client.handle(duplicate) {
		t.Fatal("duplicate hello was retained")
	}
	duplicate.ReturnToVTPool()
	_ = client.Close()
	if len(client.ready) != 0 {
		t.Fatal("closed worker retained a pending handshake lease")
	}
}

func TestMaliciousStreamFloodFailsClosedAtSingleFrameQueue(t *testing.T) {
	client := parentClientFixture(t)
	stream, err := client.reserveStream()
	if err != nil {
		t.Fatal(err)
	}
	first := wire.FrameFromVTPool()
	first.Kind = streamFrame
	first.Stream = stream.id
	if !client.deliverStream(first) {
		first.ReturnToVTPool()
		t.Fatal("first stream frame refused")
	}
	second := wire.FrameFromVTPool()
	second.Kind = streamFrame
	second.Stream = stream.id
	if client.deliverStream(second) {
		t.Fatal("worker bypassed its acknowledgement window")
	}
	second.ReturnToVTPool()
	if client.peer.ctx.Err() == nil || len(stream.frames) != 1 {
		t.Fatal("unacknowledged stream flood grew parent queue")
	}
	_ = stream.Close()
	if len(stream.frames) != 0 {
		t.Fatal("stream close retained the outstanding frame lease")
	}
}

func TestParentTypedEventLeaseSurvivesWorkerEnvelopeReturn(t *testing.T) {
	client := parentClientFixture(t)
	stream, err := client.reserveStream()
	if err != nil {
		t.Fatal(err)
	}
	frame := wire.FrameFromVTPool()
	frame.Kind = streamFrame
	frame.Stream = stream.id
	frame.Message = protocol.ServerMessageFromVTPool()
	frame.Message.Version = protocol.Version
	frame.Message.Kind = protocol.EventMessage
	frame.Message.Event = protocol.EventFromVTPool()
	frame.Message.Event.Sequence = 1
	frame.Message.Event.Kind = protocol.EventKind_CHAT
	frame.Message.Event.Chat = protocol.ChatEventFromVTPool()
	frame.Message.Event.Chat.ChannelId = "channel"
	frame.Message.Event.Chat.UserId = "user"
	frame.Message.Event.Chat.Text = "owned-event"
	if !client.deliverStream(frame) {
		frame.ReturnToVTPool()
		t.Fatal("typed frame refused")
	}
	destination := protocol.ServerMessageFromVTPool()
	defer destination.ReturnToVTPool()
	if err = stream.Recv(t.Context(), destination); err != nil {
		t.Fatal(err)
	}
	if destination.Event == nil || destination.Event.Chat == nil || destination.Event.Chat.Text != "owned-event" {
		t.Fatal("returning worker envelope reclaimed the consumer's event lease")
	}
}

func TestUnknownPrivateOperationsStopBothBoundaries(t *testing.T) {
	p := newPeer(t.Context(), strings.NewReader(""), &bytes.Buffer{})
	client := &Client{peer: p}
	if client.handle(&wire.Frame{Kind: 999}) || p.ctx.Err() == nil {
		t.Fatal("parent accepted unknown worker capability")
	}
	child := newPeer(context.Background(), strings.NewReader(""), &bytes.Buffer{})
	w := &worker{peer: child}
	if w.handle(&wire.Frame{Kind: 999}) || child.ctx.Err() == nil {
		t.Fatal("worker accepted unknown parent capability")
	}
}

func TestVirtualDialerCannotSelectArbitraryAddress(t *testing.T) {
	d := &virtualDialer{origins: [2]string{"https://auth.example", "https://events.example:8443"}}
	for _, address := range []string{"evil.example:443", "auth.example:80", "auth.example.evil:443", "127.0.0.1:443", "events.example:443"} {
		if _, allowed := d.origin(address); allowed {
			t.Fatal("worker selected a raw TCP address outside its configured capabilities")
		}
	}
	if origin, allowed := d.origin("events.example:8443"); !allowed || origin != 1 {
		t.Fatal("configured raw TCP capability was not selectable")
	}
}

func TestFailedSandboxStartsHaveFixedBudgetWithoutDirectFallback(t *testing.T) {
	life, cancel := context.WithCancel(t.Context())
	s := &Supervisor{ctx: life, cancel: cancel, gate: make(chan struct{}, 1), config: Config{Executable: filepath.Join(t.TempDir(), "missing-worker"), AuthEndpoint: "https://auth.example"}}
	t.Cleanup(func() { _ = s.Close() })
	for attempt := 0; attempt < 4; attempt++ {
		client, err := s.current(t.Context())
		if err == nil || client != nil || s.worker != nil {
			t.Fatal("failed sandbox start returned a networking fallback")
		}
	}
	if s.attempts != 3 {
		t.Fatal("sandbox restart budget exceeded three starts")
	}
	s.window = time.Now().Add(-31 * time.Second)
	_, _ = s.current(t.Context())
	if s.attempts != 1 {
		t.Fatal("sandbox restart budget did not reset after its bounded window")
	}
}
