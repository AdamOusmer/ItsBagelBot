// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package sesamerpc

import "ItsBagelBot/internal/domain/rpc"

const VoiceTestSubject = "rpc.voice.test"

type VoiceTestRequest struct {
	ActorID           string `json:"actor_id"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
	DisplayName       string `json:"display_name"`
}

func (r VoiceTestRequest) Requested() string { return r.BroadcasterUserID }

type VoiceTestStatus string

const (
	VoiceTestQueued      VoiceTestStatus = "queued"
	VoiceTestNoLink      VoiceTestStatus = "no_link"
	VoiceTestNoMinutes   VoiceTestStatus = "no_minutes"
	VoiceTestRateLimited VoiceTestStatus = "rate_limited"
	VoiceTestDisabled    VoiceTestStatus = "disabled"
)

type VoiceTestReply struct {
	Status VoiceTestStatus `json:"status,omitempty"`
	rpc.Refusal
}
