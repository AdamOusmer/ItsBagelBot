// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package transport

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"
	"ItsBagelBot/pkg/websocket/testkit"
)

type appStream struct {
	call     testkit.Call
	messages chan *protocol.Message
	end      chan struct{}
}
type appCall struct {
	call testkit.Call
	body []byte
}

func appFixture(t *testing.T, text bool) (*Client, *testkit.Server, chan *appStream, chan appCall) {
	t.Helper()
	streams := make(chan *appStream, 4)
	calls := make(chan appCall, 4)
	service := testkit.Service{StreamPath: protocol.EventsPath, Subprotocol: protocol.Subprotocol, TextFrames: text, MaxMessageBytes: protocol.MaxFrameBytes, Authorize: func(call testkit.Call) int {
		if call.Token != "native" {
			return http.StatusUnauthorized
		}
		return 0
	}, Posts: map[string]func(context.Context, testkit.Call, []byte) int{
		protocol.BindPath: func(_ context.Context, call testkit.Call, body []byte) int {
			calls <- appCall{call: call, body: append([]byte(nil), body...)}
			return http.StatusNoContent
		},
	}}
	service.HandleStream = func(ctx context.Context, call testkit.Call, send func([]byte) error) error {
		peer := &appStream{call: call, messages: make(chan *protocol.Message, 4), end: make(chan struct{})}
		select {
		case streams <- peer:
		case <-ctx.Done():
			return ctx.Err()
		}
		return serveAppStream(ctx, peer, send)
	}
	server := testkit.New(t, service)
	client, err := NewDirectClient(Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client, server, streams, calls
}
func encode(m protobuf.Message) ([]byte, error) {
	size, err := protobuf.Size(m)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, size)
	_, err = protobuf.MarshalTo(raw, m)
	return raw, err
}
func nextAppStream(t *testing.T, streams chan *appStream) *appStream {
	t.Helper()
	select {
	case peer := <-streams:
		return peer
	case <-time.After(time.Second):
		t.Fatal("WSS upgrade missing")
		return nil
	}
}
func openAppStream(t *testing.T, client *Client) Conn {
	t.Helper()
	conn, err := client.Dial(t.Context(), Request{Token: "native"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func TestTypedTransportBindsAfterStarted(t *testing.T) {
	client, server, streams, calls := appFixture(t, false)
	conn := openAppStream(t, client)
	peer := nextAppStream(t, streams)
	if peer.call.Token != "native" || len(peer.call.Headers["X-Bagel-After"]) != 0 || len(peer.call.Headers["X-Bagel-Migration"]) != 0 {
		t.Fatal("initial socket carried account binding")
	}
	peer.messages <- &protocol.Message{Version: 1, Kind: protocol.SessionStarted, SessionId: "session", BindWithinMs: 1000}
	message := protocol.ServerMessageFromVTPool()
	defer message.ReturnToVTPool()
	if err := conn.Recv(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatal("control sent before explicit account bind")
	}
	if err := client.Bind(t.Context(), BindRequest{Token: "native", SessionID: message.SessionId, Migration: "move", After: 9}); err != nil {
		t.Fatal(err)
	}
	assertAppBinding(t, <-calls)
	if server.PhysicalConnections() != 2 {
		t.Fatalf("expected WSS plus native HTTPS control connection, got %d", server.PhysicalConnections())
	}
	if err := client.Bind(t.Context(), BindRequest{Token: "native", SessionID: "session"}); err != nil {
		t.Fatal(err)
	}
	<-calls
	if server.PhysicalConnections() != 2 {
		t.Fatal("healthy HTTPS connection was not reused")
	}
}
func TestTypedTransportIndependentSockets(t *testing.T) {
	client, server, streams, _ := appFixture(t, false)
	primary := openAppStream(t, client)
	old := nextAppStream(t, streams)
	pending := openAppStream(t, client)
	next := nextAppStream(t, streams)
	primary.Close()
	next.messages <- &protocol.Message{Version: 1, Kind: protocol.Keepalive}
	m := protocol.ServerMessageFromVTPool()
	defer m.ReturnToVTPool()
	if err := pending.Recv(t.Context(), m); err != nil || m.Kind != protocol.Keepalive {
		t.Fatal("closing primary disrupted candidate", err)
	}
	if server.PhysicalConnections() != 2 || old == next {
		t.Fatal("overlap did not use independent sockets")
	}
}
func TestTypedTransportRejectsBadTokenAndText(t *testing.T) {
	client, _, _, _ := appFixture(t, false)
	if _, err := client.Dial(t.Context(), Request{Token: "bad"}); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong auth classification: %v", err)
	}
	textClient, _, streams, _ := appFixture(t, true)
	conn := openAppStream(t, textClient)
	peer := nextAppStream(t, streams)
	peer.messages <- &protocol.Message{Version: 1, Kind: protocol.Keepalive}
	m := protocol.ServerMessageFromVTPool()
	defer m.ReturnToVTPool()
	if err := conn.Recv(t.Context(), m); err == nil || errors.Is(err, ErrAuthentication) {
		t.Fatal("text frame accepted or treated as account rejection")
	}
}
func TestTypedTransportFrameLimit(t *testing.T) {
	client, _, streams, _ := appFixture(t, false)
	conn := openAppStream(t, client)
	peer := nextAppStream(t, streams)
	peer.messages <- &protocol.Message{Version: 1, Kind: protocol.SessionStarted, SessionId: strings.Repeat("a", protocol.MaxFrameBytes+1), BindWithinMs: 1000}
	m := protocol.ServerMessageFromVTPool()
	defer m.ReturnToVTPool()
	if err := conn.Recv(t.Context(), m); !errors.Is(err, shared.ErrTooLarge) {
		t.Fatalf("frame limit: %v", err)
	}
}

func serveAppStream(ctx context.Context, peer *appStream, send func([]byte) error) error {
	for {
		select {
		case m := <-peer.messages:
			raw, err := encode(m)
			if err != nil {
				return err
			}
			if err = send(raw); err != nil {
				return err
			}
		case <-peer.end:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func assertAppBinding(t *testing.T, control appCall) {
	t.Helper()
	var bound protocol.BindAccount
	if err := protobuf.Unmarshal(control.body, &bound); err != nil {
		t.Fatal(err)
	}
	if bound.SessionId != "session" || bound.After != 9 || bound.Migration != "move" || control.call.Token != "native" {
		t.Fatal("typed native account binding changed")
	}
	if strings.Contains(string(control.body), "native") {
		t.Fatal("account credential serialized into message")
	}
}
