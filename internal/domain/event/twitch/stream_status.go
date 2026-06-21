package twitch

import (
	"encoding/json"
	"strconv"
)

// StreamStatus holds the decoded stream state from a Twitch EventSub message.
type StreamStatus struct {
	BroadcasterID uint64
	Live          bool
}

// eventSubEnvelope is a private struct that mirrors the Twitch EventSub wire
// shape. It is used only inside DecodeStreamStatus so that Twitch JSON field
// names stay within this package.
type eventSubEnvelope struct {
	Type         string `json:"type"`
	Subscription struct {
		Type string `json:"type"`
	} `json:"subscription"`
	Event struct {
		BroadcasterUserID string `json:"broadcaster_user_id"`
	} `json:"event"`
}

func (e eventSubEnvelope) effectiveType() string {
	if e.Type != "" {
		return e.Type
	}
	return e.Subscription.Type
}

// DecodeStreamStatus parses a raw Twitch EventSub JSON payload and returns the
// stream status. Only "stream.online" and "stream.offline" event types are
// accepted; anything else returns (zero, false). Malformed JSON or an
// unparseable broadcaster ID also returns (zero, false).
func DecodeStreamStatus(raw []byte) (StreamStatus, bool) {
	var env eventSubEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return StreamStatus{}, false
	}

	et := env.effectiveType()
	if et != "stream.online" && et != "stream.offline" {
		return StreamStatus{}, false
	}

	id, err := strconv.ParseUint(env.Event.BroadcasterUserID, 10, 64)
	if err != nil {
		return StreamStatus{}, false
	}

	return StreamStatus{
		BroadcasterID: id,
		Live:          et == "stream.online",
	}, true
}
