// Package lane holds the shared wire contract ingress publishes on the premium,
// standard and stream lanes and the worker consumes. It is the single source of
// the lane message shape: ingress writes it (see app/ingress/lib/ingress/
// pipeline.ex) and every Go consumer decodes this type rather than redefining it.
package lane

import (
	"encoding/json"
	"strconv"
)

// Badge is one Twitch chat badge as carried on a channel.chat.message event.
// The set_id identifies the role ("broadcaster", "moderator", "lead_moderator",
// "vip", "subscriber", "founder", ...); id/info are Twitch's per-badge detail.
type Badge struct {
	SetID string `json:"set_id"`
	ID    string `json:"id,omitempty"`
	Info  string `json:"info,omitempty"`
}

// Envelope is the wire contract published by ingress. Consumers read exactly the
// fields ingress writes. Every event carries its Twitch EventSub `type` and the
// `lane` it was routed on; the rest depends on the type:
//
//   - channel.chat.message is flattened (broadcaster/chatter ids, text, badges);
//   - every other type, including stream.online/offline, nests the raw EventSub
//     `event` object.
type Envelope struct {
	Type string `json:"type"`
	Lane string `json:"lane"`

	// Flattened chat fields (only set for channel.chat.message).
	BroadcasterUserID    string  `json:"broadcaster_user_id,omitempty"`
	BroadcasterUserLogin string  `json:"broadcaster_user_login,omitempty"`
	ChatterUserID        string  `json:"chatter_user_id,omitempty"`
	ChatterUserLogin     string  `json:"chatter_user_login,omitempty"`
	Text                 string  `json:"text,omitempty"`
	Badges               []Badge `json:"badges,omitempty"`

	// Raw EventSub event object (set for every non-chat type).
	Event json.RawMessage `json:"event,omitempty"`

	MsgID   string `json:"msg_id,omitempty"`
	ShardID int    `json:"shard_id,omitempty"`
}

// BroadcasterID returns the broadcaster the event belongs to as a uint64. For
// chat it is the flattened field; for every other type it is read from the raw
// event (raids name the receiving channel as to_broadcaster_user_id).
func (e Envelope) BroadcasterID() (uint64, bool) {
	raw := e.BroadcasterUserID
	if raw == "" {
		var ev struct {
			BroadcasterUserID   string `json:"broadcaster_user_id"`
			ToBroadcasterUserID string `json:"to_broadcaster_user_id"`
		}
		if len(e.Event) > 0 {
			_ = json.Unmarshal(e.Event, &ev)
		}
		raw = ev.BroadcasterUserID
		if raw == "" {
			raw = ev.ToBroadcasterUserID
		}
	}
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
