// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testkit

import (
	"context"
	"io"
	"testing"
	"time"

	shared "ItsBagelBot/pkg/grpc"
	"ItsBagelBot/pkg/grpc/internal/testpb"
)

type testPeer struct {
	call   Call
	frames chan *testpb.Packet
	end    chan struct{}
}

func fixtureService(peers chan *testPeer) Service {
	return Service{Name: "fixture.Test", Streams: []StreamMethod{{Name: "Listen", NewRequest: func() shared.Message { return &shared.Empty{} }, Handle: func(ctx context.Context, call Call, _ shared.Message, send func(shared.Message) error) error {
		if call.Token == "bad" {
			return shared.ErrAuthentication
		}
		peer := &testPeer{call: call, frames: make(chan *testpb.Packet, 2), end: make(chan struct{})}
		select {
		case peers <- peer:
		case <-ctx.Done():
			return ctx.Err()
		}
		return fixtureDelivery(ctx, peer, send)
	}}}, Methods: []UnaryMethod{{Name: "Control", NewRequest: func() shared.Message { return &testpb.Packet{} }, Handle: func(_ context.Context, _ Call, request shared.Message) (shared.Message, error) {
		return &testpb.Packet{Sequence: request.(*testpb.Packet).Sequence + 1}, nil
	}}}}
}
func fixtureDelivery(ctx context.Context, peer *testPeer, send func(shared.Message) error) error {
	for {
		select {
		case frame := <-peer.frames:
			if err := send(frame); err != nil {
				return err
			}
		case <-peer.end:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func nextPeer(t *testing.T, peers chan *testPeer) *testPeer {
	t.Helper()
	select {
	case peer := <-peers:
		return peer
	case <-time.After(time.Second):
		t.Fatal("fixture did not receive stream")
		return nil
	}
}
func fixtureClient(t *testing.T, server *Server) *shared.Client {
	t.Helper()
	client, err := shared.NewClient(shared.Config{Endpoint: server.Endpoint, TLSConfig: server.TLSConfig, MaxMessageBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func fixtureListen(t *testing.T, client *shared.Client) *shared.Stream {
	t.Helper()
	stream, err := client.Listen(t.Context(), "/fixture.Test/Listen", shared.Credentials{Token: "native"}, &shared.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	return stream
}
func fixtureReceive(t *testing.T, stream *shared.Stream, sequence uint64) {
	t.Helper()
	var packet testpb.Packet
	if err := stream.Recv(t.Context(), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Sequence != sequence {
		t.Fatalf("sequence %d, want %d", packet.Sequence, sequence)
	}
}
func TestReplacementDrainsOldRPCWithoutDeliveryGap(t *testing.T) {
	peers := make(chan *testPeer, 2)
	service := fixtureService(peers)
	original := New(t, service)
	client := fixtureClient(t, original)
	primary := fixtureListen(t, client)
	old := nextPeer(t, peers)
	replacement := original.Replace(t, service)
	old.frames <- &testpb.Packet{Sequence: 1}
	fixtureReceive(t, primary, 1)
	pending := fixtureListen(t, client)
	next := nextPeer(t, peers)
	next.frames <- &testpb.Packet{Sequence: 2}
	fixtureReceive(t, pending, 2)
	old.frames <- &testpb.Packet{Sequence: 3}
	fixtureReceive(t, primary, 3)
	_ = primary.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := original.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	next.frames <- &testpb.Packet{Sequence: 4}
	fixtureReceive(t, pending, 4)
	if original.PhysicalConnections() != 1 || replacement.PhysicalConnections() != 1 {
		t.Fatal("native replacement did not move to one new physical channel")
	}
	close(next.end)
	if err := pending.Recv(t.Context(), &testpb.Packet{}); err != io.EOF {
		t.Fatalf("replacement EOF: %v", err)
	}
}
