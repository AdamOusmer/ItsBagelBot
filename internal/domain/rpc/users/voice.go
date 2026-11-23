// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/voiceplan"
)

const VoiceRequestIDMaxBytes = 255
const AdminVoiceUsageAuditAction = "voice_usage_set"

const (
	VoiceCheckSubject  = "rpc.internal.users.voice.check"
	VoiceRecordSubject = "rpc.internal.users.voice.record"
)

type VoiceAllowance struct {
	Plan            voiceplan.Plan `json:"plan"`
	Period          string         `json:"period"`
	LimitTokens     int64          `json:"limit_tokens"`
	UsedTokens      int64          `json:"used_tokens"`
	RemainingTokens int64          `json:"remaining_tokens"`
	ResetsAt        time.Time      `json:"resets_at"`
	Reason          string         `json:"reason"`
}

type VoiceGetRequest struct {
	ActorID           string `json:"actor_id"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type VoiceCheckRequest struct {
	UserID string `json:"user_id"`
}

func (r VoiceCheckRequest) Requested() string { return r.UserID }

type VoiceRecordRequest struct {
	UserID      string `json:"user_id"`
	RequestID   string `json:"request_id"`
	AudioTokens int64  `json:"audio_tokens"`
	DurationMS  int64  `json:"duration_ms"`
}

func (r VoiceRecordRequest) Requested() string { return r.UserID }

type VoiceReply struct {
	VoiceAllowance
	rpc.Refusal
}

type VoiceCheckReply struct {
	VoiceReply
	Allowed bool `json:"allowed"`
}

type VoiceUnblockRequest struct {
	VoiceGetRequest
	ViewerID string `json:"viewer_id"`
}

type VoiceStrikeBlock struct {
	ViewerID string `json:"viewer_id"`
	Scope    string `json:"scope"`
	Until    int64  `json:"until"`
}

type VoiceSafetyReply struct {
	PausedUntil int64              `json:"paused_until"`
	Blocks      []VoiceStrikeBlock `json:"blocks"`
	rpc.Refusal
}
