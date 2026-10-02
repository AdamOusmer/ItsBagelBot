// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"strconv"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/codec"
)

type StreamStatus struct {
	BroadcasterID uint64
	Live          bool
	Version       int64
}

type eventSubEnvelope struct {
	Type         string `json:"type"`
	Subscription struct {
		Type string `json:"type"`
	} `json:"subscription"`
	Event struct {
		BroadcasterUserID string `json:"broadcaster_user_id"`
	} `json:"event"`
	ReceivedAt string `json:"received_at"`
}

func (e eventSubEnvelope) effectiveType() string {
	if e.Type != "" {
		return e.Type
	}
	return e.Subscription.Type
}

func DecodeStreamStatus(raw []byte) (StreamStatus, bool) {
	var env eventSubEnvelope
	if err := codec.Unmarshal(raw, &env); err != nil {
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
		Version:       lane.Envelope{ReceivedAt: env.ReceivedAt}.EventVersion(),
	}, true
}
