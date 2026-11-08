// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package protocol

import (
	"errors"
	"strings"
	"testing"

	sharedproto "ItsBagelBot/pkg/protobuf"
)

var _ sharedproto.Message = (*ServerMessage)(nil)
var _ sharedproto.Message = (*BindAccount)(nil)
var _ sharedproto.Message = (*MigrationRequest)(nil)
var _ sharedproto.Message = (*Event)(nil)

func chatMessage() *Message {
	return &Message{Version: Version, Kind: EventMessage, Event: &Event{Sequence: 42, Kind: EventKind_CHAT,
		Chat: &ChatEvent{ChannelId: "channel", UserId: "user", DisplayName: "Bagel Fan", Text: "Fresh bagels 🥯"}}}
}

func TestValidateMessages(t *testing.T) {
	for _, message := range []*Message{
		{Version: Version, Kind: SessionStarted, SessionId: "session", BindWithinMs: 10000},
		{Version: Version, Kind: SessionBound, SessionId: "session"},
		{Version: Version, Kind: Reconnect, Migration: "migration", WithinMs: 30000, Target: "https://bridge.example"},
		{Version: Version, Kind: HandoffReady, Migration: "migration", Through: 42},
		{Version: Version, Kind: HandoffReady, Migration: "migration", Through: 0},
		{Version: Version, Kind: AuthError, Code: "revoked"},
		{Version: Version, Kind: Keepalive}, chatMessage(),
		{Version: Version, Kind: EventMessage, Event: &Event{Sequence: 43, Kind: EventKind_ALERT, Alert: &AlertEvent{Kind: "subscription", Name: "Bagel Fan", Amount: 12}}},
	} {
		if err := Validate(message); err != nil {
			t.Fatalf("kind %s: %v", message.Kind, err)
		}
	}
}

func TestRejectInvalidMessages(t *testing.T) {
	for _, message := range []*Message{
		nil, {}, {Version: 2, Kind: Keepalive}, {Version: Version, Kind: Kind(100)},
		{Version: Version, Kind: SessionStarted, SessionId: "session"},
		{Version: Version, Kind: SessionStarted, SessionId: "session", BindWithinMs: MaxWindowMS + 1},
		{Version: Version, Kind: SessionBound, SessionId: "session", Through: 1},
		{Version: Version, Kind: Keepalive, Event: &Event{}},
		{Version: Version, Kind: SessionBound, SessionId: strings.Repeat("a", 513)},
		{Version: Version, Kind: Reconnect, Migration: "migration", WithinMs: 1, Target: strings.Repeat("a", 2049)},
		{Version: Version, Kind: AuthError, Code: string([]byte{0xff})},
	} {
		if err := Validate(message); err == nil {
			t.Fatalf("accepted %+v", message)
		}
	}
}

func encoded(t testing.TB, message sharedproto.Message) []byte {
	t.Helper()
	size, err := sharedproto.Size(message)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, size)
	if _, err = sharedproto.MarshalTo(raw, message); err != nil {
		t.Fatal(err)
	}
	return raw
}

func addUnknown(t testing.TB, message sharedproto.Message) {
	t.Helper()
	// Syntactically valid field 100, varint 1. Strict app contracts must reject
	// it even though the shared protobuf decoder preserves it successfully.
	raw := append(encoded(t, message), 0xa0, 0x06, 0x01)
	if err := sharedproto.Unmarshal(raw, message); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownWireFieldsRejectedAtEveryMessageBoundary(t *testing.T) {
	for _, location := range []string{"envelope", "event", "chat", "alert"} {
		t.Run(location, func(t *testing.T) {
			message := chatMessage()
			var target sharedproto.Message = message
			switch location {
			case "event":
				target = message.Event
			case "chat":
				target = message.Event.Chat
			case "alert":
				message.Event = &Event{Sequence: 1, Kind: EventKind_ALERT, Alert: &AlertEvent{Kind: "subscription"}}
				target = message.Event.Alert
			}
			addUnknown(t, target)
			received := ServerMessageFromVTPool()
			defer received.ReturnToVTPool()
			if err := sharedproto.Unmarshal(encoded(t, message), received); err != nil {
				t.Fatal(err)
			}
			if err := Validate(received); !errors.Is(err, ErrField) {
				t.Fatal("unknown wire field escaped schema boundary", err)
			}
		})
	}
}

func TestUnknownTypedControlFieldsRejected(t *testing.T) {
	bind := &BindAccount{SessionId: "session"}
	addUnknown(t, bind)
	if err := ValidateBind(bind); !errors.Is(err, ErrField) {
		t.Fatal("unknown bind field accepted", err)
	}
	migration := &MigrationRequest{SessionId: "session"}
	addUnknown(t, migration)
	if err := ValidateMigrationRequest(migration); !errors.Is(err, ErrField) {
		t.Fatal("unknown migration field accepted", err)
	}
}

func FuzzValidatedMessageRoundTrip(f *testing.F) {
	f.Add(encoded(f, chatMessage()))
	f.Add(encoded(f, &Message{Version: Version, Kind: Keepalive}))
	f.Add([]byte{0x80})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxFrameBytes {
			t.Skip()
		}
		message := ServerMessageFromVTPool()
		defer message.ReturnToVTPool()
		if sharedproto.Unmarshal(raw, message) != nil || Validate(message) != nil {
			return
		}
		// Validated data must remain valid after a safe, input-owning round trip.
		copy := ServerMessageFromVTPool()
		defer copy.ReturnToVTPool()
		if err := sharedproto.Unmarshal(encoded(t, message), copy); err != nil {
			t.Fatal(err)
		}
		clear(raw)
		if err := Validate(copy); err != nil {
			t.Fatal("validated typed message changed after wire ownership transfer", err)
		}
	})
}

func TestRejectInvalidTypedEvents(t *testing.T) {
	for _, event := range []*Event{
		nil, {Sequence: 1, Kind: EventKind(99)},
		{Kind: EventKind_CHAT, Chat: &ChatEvent{ChannelId: "c", UserId: "u"}},
		{Sequence: 1, Kind: EventKind_CHAT},
		{Sequence: 1, Kind: EventKind_CHAT, Chat: &ChatEvent{ChannelId: "c", UserId: "u"}, Alert: &AlertEvent{}},
		{Sequence: 1, Kind: EventKind_CHAT, Chat: &ChatEvent{ChannelId: "c", UserId: "u", Text: strings.Repeat("a", (16<<10)+1)}},
		{Sequence: 1, Kind: EventKind_CHAT, Chat: &ChatEvent{ChannelId: "c", UserId: "u", DisplayName: string([]byte{0xff})}},
		{Sequence: 1, Kind: EventKind_ALERT, Alert: &AlertEvent{Kind: strings.Repeat("a", 257)}},
		{Sequence: 1, Kind: EventKind_ALERT, Alert: &AlertEvent{Kind: "subscription", Name: strings.Repeat("a", 257)}},
		{Sequence: 1, Kind: EventKind_ALERT, Chat: &ChatEvent{}, Alert: &AlertEvent{Kind: "subscription"}},
	} {
		if err := ValidateEvent(event); err == nil {
			t.Fatalf("accepted %+v", event)
		}
	}
}

func TestSafeWireDecodeAndPoolReset(t *testing.T) {
	want := chatMessage()
	wire, err := want.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	message := ServerMessageFromVTPool()
	defer message.ReturnToVTPool()
	if err = message.UnmarshalVT(wire); err != nil {
		t.Fatal(err)
	}
	clear(wire)
	if err = Validate(message); err != nil {
		t.Fatal(err)
	}
	if message.Event.Chat.Text != want.Event.Chat.Text {
		t.Fatal("received strings alias released wire")
	}
	message.ResetVT()
	next := &Message{Version: Version, Kind: Keepalive}
	wire, err = next.MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	if err = message.UnmarshalVT(wire); err != nil {
		t.Fatal(err)
	}
	if err = Validate(message); err != nil {
		t.Fatal(err)
	}
	if message.Event != nil {
		t.Fatal("pooled message retained previous account payload")
	}
}

func TestDetachedEventOwnership(t *testing.T) {
	wire, err := chatMessage().MarshalVT()
	if err != nil {
		t.Fatal(err)
	}
	message := ServerMessageFromVTPool()
	if err = message.UnmarshalVT(wire); err != nil {
		t.Fatal(err)
	}
	payload := message.Event
	message.Event = nil
	message.ReturnToVTPool()
	defer payload.ReturnToVTPool()
	if err = ValidateEvent(payload); err != nil {
		t.Fatal(err)
	}
	if payload.Chat.Text != "Fresh bagels 🥯" {
		t.Fatal("releasing server envelope released transferred event")
	}
}

func TestControlValidation(t *testing.T) {
	if err := ValidateBind(&BindAccount{SessionId: "session", Migration: "migration", After: 42}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMigrationRequest(&MigrationRequest{SessionId: "session", After: 42}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBind(nil); err == nil {
		t.Fatal("accepted nil bind")
	}
	if err := ValidateBind(&BindAccount{SessionId: "session", Migration: strings.Repeat("x", 513)}); err == nil {
		t.Fatal("accepted oversized migration")
	}
	if err := ValidateMigrationRequest(nil); err == nil {
		t.Fatal("accepted nil migration request")
	}
}

func TestValidationAllocationBudget(t *testing.T) {
	message := chatMessage()
	allocations := testing.AllocsPerRun(1000, func() {
		if err := Validate(message); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("typed validation allocated %g times", allocations)
	}
}

func BenchmarkValidate(b *testing.B) {
	message := chatMessage()
	b.ReportAllocs()
	for b.Loop() {
		if err := Validate(message); err != nil {
			b.Fatal(err)
		}
	}
}
