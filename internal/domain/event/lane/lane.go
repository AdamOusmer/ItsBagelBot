// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package lane

import (
	"ItsBagelBot/pkg/codec"
	"strconv"
	"time"
)

type Badge struct {
	SetID string `json:"set_id"`
	ID    string `json:"id,omitempty"`
	Info  string `json:"info,omitempty"`
}

type Sender struct {
	ChatterUserID    string  `json:"chatter_user_id,omitempty"`
	ChatterUserLogin string  `json:"chatter_user_login,omitempty"`
	MsgID            string  `json:"msg_id,omitempty"`
	TS               string  `json:"ts,omitempty"`
	Badges           []Badge `json:"badges,omitempty"`
}

type EmoteSpan struct {
	ID    string `json:"id"`
	Begin int    `json:"begin"`
	End   int    `json:"end"`
}

// Built by app/twitch/ingress (Elixir); change both sides together.
type Envelope struct {
	Type            string `json:"type"`
	Lane            string `json:"lane"`
	EventID         string `json:"event_id,omitempty"`
	Origin          string `json:"origin,omitempty"`
	TrialGeneration uint64 `json:"trial_generation,omitempty"`

	BroadcasterUserID    string      `json:"broadcaster_user_id,omitempty"`
	BroadcasterUserLogin string      `json:"broadcaster_user_login,omitempty"`
	BroadcasterUserName  string      `json:"broadcaster_user_name,omitempty"`
	ChatterUserID        string      `json:"chatter_user_id,omitempty"`
	ChatterUserLogin     string      `json:"chatter_user_login,omitempty"`
	ChatterUserName      string      `json:"chatter_user_name,omitempty"`
	Text                 string      `json:"text,omitempty"`
	Badges               []Badge     `json:"badges,omitempty"`
	Emotes               []EmoteSpan `json:"emotes,omitempty"`

	Senders []Sender `json:"senders,omitempty"`

	Event codec.RawMessage `json:"event,omitempty"`

	MsgID         string `json:"msg_id,omitempty"`
	ChatMessageID string `json:"chat_message_id,omitempty"`
	ShardID       int    `json:"shard_id,omitempty"`

	ReceivedAt string `json:"received_at,omitempty"`
}

func (e Envelope) BroadcasterName() string {
	if e.BroadcasterUserName != "" {
		return e.BroadcasterUserName
	}
	return e.BroadcasterUserLogin
}

func (e Envelope) ChatterName() string {
	if e.ChatterUserName != "" {
		return e.ChatterUserName
	}
	return e.ChatterUserLogin
}

func (e Envelope) EventVersion() int64 {
	if e.ReceivedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, e.ReceivedAt)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func (e Envelope) BroadcasterID() (uint64, bool) {
	raw := e.BroadcasterUserID
	if raw == "" {
		var ev struct {
			BroadcasterUserID   string `json:"broadcaster_user_id"`
			ToBroadcasterUserID string `json:"to_broadcaster_user_id"`
		}
		if len(e.Event) > 0 {
			_ = codec.Unmarshal(e.Event, &ev)
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
