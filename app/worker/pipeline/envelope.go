package pipeline

import (
	"encoding/json"
	"strconv"
)

// Envelope is the wire contract published by Ingress.Pipeline. The worker
// reads exactly the fields ingress writes (see app/ingress/lib/ingress/
// pipeline.ex). Every event carries its Twitch EventSub `type` and the `lane`
// it was routed on; the rest depends on the type:
//
//   - channel.chat.message is flattened (broadcaster/chatter ids, text);
//   - every other type, including stream.online/offline, nests the raw
//     EventSub `event` object.
type Envelope struct {
	Type string `json:"type"`
	Lane string `json:"lane"`

	// Flattened chat fields (only set for channel.chat.message).
	BroadcasterUserID    string `json:"broadcaster_user_id,omitempty"`
	BroadcasterUserLogin string `json:"broadcaster_user_login,omitempty"`
	ChatterUserID        string `json:"chatter_user_id,omitempty"`
	ChatterUserLogin     string `json:"chatter_user_login,omitempty"`
	Text                 string `json:"text,omitempty"`

	// Raw EventSub event object (set for every non-chat type).
	Event json.RawMessage `json:"event,omitempty"`

	MsgID   string `json:"msg_id,omitempty"`
	ShardID int    `json:"shard_id,omitempty"`
}

// Regress is the lane an event arrived on. It is the worker's "regress status":
// it records whether ingress decided this traffic was premium or standard, so
// the pipeline can keep the same priority all the way out to outgress without
// re-deciding it. Stream is the live lane and carries no priority of its own.
type Regress int

const (
	RegressStandard Regress = iota
	RegressPremium
	RegressStream
)

func (r Regress) String() string {
	switch r {
	case RegressPremium:
		return "premium"
	case RegressStream:
		return "stream"
	default:
		return "standard"
	}
}

// regressFromLane maps the ingress lane string onto the regress status.
func regressFromLane(lane string) Regress {
	switch lane {
	case "premium":
		return RegressPremium
	case "stream":
		return RegressStream
	default:
		return RegressStandard
	}
}

// broadcasterID returns the broadcaster the event belongs to as a uint64. For
// chat it is the flattened field; for every other type it is read from the raw
// event (raids name the receiving channel as to_broadcaster_user_id).
func (e Envelope) broadcasterID() (uint64, bool) {
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
