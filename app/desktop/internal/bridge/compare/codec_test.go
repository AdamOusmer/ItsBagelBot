// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package compare

import (
	"strings"
	"testing"

	"ItsBagelBot/pkg/codec"

	"google.golang.org/protobuf/proto"
)

// JSONEvent and the generated Event carry the same typed semantics. The JSON
// side uses the repository codec; protobuf does not contain JSON or a byte tunnel.
type JSONEvent struct {
	Sequence     uint64          `json:"sequence"`
	SentUnixNano int64           `json:"sent_unix_nano"`
	Kind         EventKind       `json:"kind"`
	Chat         *JSONChatEvent  `json:"chat,omitempty"`
	Alert        *JSONAlertEvent `json:"alert,omitempty"`
}

type JSONChatEvent struct {
	ChannelID   string `json:"channel_id"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Text        string `json:"text"`
}

type JSONAlertEvent struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Amount int64  `json:"amount"`
}

func SampleEvent(name string) *Event {
	event := &Event{Sequence: 123456789, SentUnixNano: 1794142800123456789, Kind: EventKind_CHAT,
		Chat: &ChatEvent{ChannelId: "123456789", UserId: "987654321", DisplayName: "BagelFan", Text: "Fresh bagels! 🥯 Thanks for the stream."}}
	switch name {
	case "chat":
	case "chat_large":
		event.Chat.Text = strings.Repeat("Overlay chat payload with unicode 🥯 and a URL https://example.com/stream. ", 64)
	case "alert":
		event.Kind = EventKind_ALERT
		event.Chat = nil
		event.Alert = &AlertEvent{Kind: "subscription", Name: "BagelFan", Amount: 12}
	default:
		panic("unknown benchmark fixture")
	}
	return event
}

func AsJSON(event *Event) JSONEvent {
	out := JSONEvent{Sequence: event.Sequence, SentUnixNano: event.SentUnixNano, Kind: event.Kind}
	if event.Chat != nil {
		out.Chat = &JSONChatEvent{ChannelID: event.Chat.ChannelId, UserID: event.Chat.UserId, DisplayName: event.Chat.DisplayName, Text: event.Chat.Text}
	}
	if event.Alert != nil {
		out.Alert = &JSONAlertEvent{Kind: event.Alert.Kind, Name: event.Alert.Name, Amount: event.Alert.Amount}
	}
	return out
}

func sameSemantic(got JSONEvent, want *Event) bool {
	if got.Sequence != want.Sequence || got.SentUnixNano != want.SentUnixNano || got.Kind != want.Kind {
		return false
	}
	if want.Chat != nil {
		return got.Chat != nil && got.Alert == nil && *got.Chat == (JSONChatEvent{ChannelID: want.Chat.ChannelId, UserID: want.Chat.UserId, DisplayName: want.Chat.DisplayName, Text: want.Chat.Text})
	}
	if want.Alert != nil {
		return got.Alert != nil && got.Chat == nil && *got.Alert == (JSONAlertEvent{Kind: want.Alert.Kind, Name: want.Alert.Name, Amount: want.Alert.Amount})
	}
	return false
}

func TestTypedPayloadEquivalence(t *testing.T) {
	for _, name := range []string{"chat", "chat_large", "alert"} {
		t.Run(name, func(t *testing.T) {
			want := SampleEvent(name)
			encoded, err := proto.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Event
			if err = proto.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(want, &decoded) {
				t.Fatal("protobuf round trip changed values")
			}
			raw, err := codec.Marshal(AsJSON(want))
			if err != nil {
				t.Fatal(err)
			}
			var jsonDecoded JSONEvent
			if err = codec.Unmarshal(raw, &jsonDecoded); err != nil {
				t.Fatal(err)
			}
			if !sameSemantic(jsonDecoded, want) {
				t.Fatal("JSON round trip changed values")
			}
			var fastDecoded JSONEvent
			if err = codec.FastUnmarshal(raw, &fastDecoded); err != nil {
				t.Fatal(err)
			}
			if !sameSemantic(fastDecoded, want) {
				t.Fatal("borrowed JSON round trip changed values")
			}
			t.Logf("protobuf=%d bytes JSON=%d bytes", len(encoded), len(raw))
		})
	}
}

func BenchmarkEncode(b *testing.B) {
	for _, name := range []string{"chat", "chat_large", "alert"} {
		b.Run(name, func(b *testing.B) {
			event := SampleEvent(name)
			jsonEvent := AsJSON(event)
			b.Run("protobuf", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					raw, err := proto.Marshal(event)
					if err != nil {
						b.Fatal(err)
					}
					if len(raw) == 0 {
						b.Fatal("empty protobuf")
					}
				}
			})
			b.Run("json", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					raw, err := codec.Marshal(&jsonEvent)
					if err != nil {
						b.Fatal(err)
					}
					if len(raw) == 0 {
						b.Fatal("empty JSON")
					}
				}
			})
			b.Run("protobuf_reuse", func(b *testing.B) {
				raw := make([]byte, 0, proto.Size(event))
				var err error
				b.ReportAllocs()
				for b.Loop() {
					raw, err = proto.MarshalOptions{}.MarshalAppend(raw[:0], event)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, name := range []string{"chat", "chat_large", "alert"} {
		b.Run(name, func(b *testing.B) {
			event := SampleEvent(name)
			protobuf, err := proto.Marshal(event)
			if err != nil {
				b.Fatal(err)
			}
			jsonData, err := codec.Marshal(AsJSON(event))
			if err != nil {
				b.Fatal(err)
			}
			b.Run("protobuf", func(b *testing.B) {
				var out Event
				b.SetBytes(int64(len(protobuf)))
				b.ReportAllocs()
				for b.Loop() {
					if err := proto.Unmarshal(protobuf, &out); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("json", func(b *testing.B) {
				var out JSONEvent
				b.SetBytes(int64(len(jsonData)))
				b.ReportAllocs()
				for b.Loop() {
					if err := codec.Unmarshal(jsonData, &out); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("json_borrowed", func(b *testing.B) {
				var out JSONEvent
				b.SetBytes(int64(len(jsonData)))
				b.ReportAllocs()
				for b.Loop() {
					if err := codec.FastUnmarshal(jsonData, &out); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
