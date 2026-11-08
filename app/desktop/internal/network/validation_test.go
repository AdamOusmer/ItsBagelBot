// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/protobuf"
)

func TestPrivateEnvelopeRejectsCrossOperationFieldsAndUnknownData(t *testing.T) {
	for name, frame := range map[string]*wire.Frame{
		"hello command":      {Kind: hello, Isolated: true, Path: "/bin/sh"},
		"hello credentials":  {Kind: hello, Isolated: true, Token: "private"},
		"lane address":       {Kind: openLane, Id: 1, Epoch: 1, Path: "https://127.0.0.1"},
		"lane outside cap":   {Kind: openLane, Id: 1, Epoch: 1, Origin: 2},
		"ack with form":      {Kind: ackStream, Stream: 1, Form: "command=whoami"},
		"reply with path":    {Kind: response, Id: 1, Path: "/etc/passwd"},
		"reply with event":   {Kind: response, Id: 1, Message: &protocol.Message{Version: protocol.Version, Kind: protocol.Keepalive}},
		"reply status":       {Kind: response, Id: 1, Status: -1},
		"reply code":         {Kind: response, Id: 1, Error: 999},
		"reply nonce":        {Kind: response, Id: 1, Nonce: "bad\r\nheader"},
		"reply token limit":  {Kind: response, Id: 1, Oauth: &wire.OAuthReply{Access: strings.Repeat("a", 8193)}},
		"missing stream":     {Kind: streamFrame, Message: &protocol.Message{Version: protocol.Version, Kind: protocol.Keepalive}},
		"empty stream error": {Kind: streamError, Stream: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if validFrame(frame) {
				t.Fatal("cross-operation or unbounded envelope accepted")
			}
		})
	}
	for _, field := range []byte{0x98, 0xa0} {
		// An unknown scalar must not be silently promoted into a future capability.
		frame := wire.FrameFromVTPool()
		raw := []byte{0x10, byte(hello), 0xa0, 0x01, 1, field, 0x06, 1}
		if err := protobuf.Unmarshal(raw, frame); err != nil {
			t.Fatal(err)
		}
		if validFrame(frame) {
			t.Fatal("unknown private wire field accepted")
		}
		frame.ReturnToVTPool()
	}
	var token wire.OAuthReply
	if err := protobuf.Unmarshal([]byte{0x98, 0x06, 1}, &token); err != nil {
		t.Fatal(err)
	}
	if validFrame(&wire.Frame{Kind: response, Id: 1, Oauth: &token}) {
		t.Fatal("unknown nested token-reply field accepted")
	}
}

func TestPrivatePeerValidatesBeforeDispatchAndEnforcesDirection(t *testing.T) {
	for _, frame := range []*wire.Frame{
		{Kind: hello, Isolated: true, Form: "command=whoami"},
		{Kind: bootstrap, Id: 1, AuthEndpoint: "https://auth.example"},
		{Kind: oauthRequest, Id: 1, Path: "https://auth.example/oauth/desktop/token", Proof: "proof", Form: "grant_type=refresh_token"},
	} {
		var input bytes.Buffer
		encoder := sender{writer: &input}
		if err := encoder.write(frame); err != nil {
			t.Fatal(err)
		}
		p := newPeer(t.Context(), &input, io.Discard)
		p.receives = parentReceives
		called := false
		p.handle = func(*wire.Frame) bool { called = true; return false }
		p.run()
		if called || p.ctx.Err() == nil {
			t.Fatal("wrong-shape/direction packet reached parent dispatch")
		}
	}
}

func TestResponseCannotChangeOutstandingCapability(t *testing.T) {
	p := newPeer(t.Context(), strings.NewReader(""), io.Discard)
	id, queue, err := p.reserve(openLane)
	if err != nil {
		t.Fatal(err)
	}
	defer p.release(id)
	frame := wire.FrameFromVTPool()
	frame.Kind, frame.Id = response, id
	frame.Oauth = &wire.OAuthReply{Access: "not-a-lane-result"}
	if p.deliver(frame) || len(queue) != 0 || p.ctx.Err() == nil {
		t.Fatal("socket capability accepted an OAuth reply")
	}
	frame.ReturnToVTPool()
}

func TestTypedPrivateValidationDoesNotAllocate(t *testing.T) {
	frame := &wire.Frame{Kind: streamFrame, Stream: 1, Message: &protocol.Message{Version: protocol.Version, Kind: protocol.Keepalive}}
	if !validFrame(frame) {
		t.Fatal("valid typed event rejected")
	}
	if allocations := testing.AllocsPerRun(1000, func() {
		if !validFrame(frame) {
			panic("valid event rejected")
		}
	}); allocations != 0 {
		t.Fatal("typed boundary validation allocated", allocations)
	}
}
