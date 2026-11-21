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
	Plan              voiceplan.Plan `json:"plan"`
	Period            string         `json:"period"`
	LimitTokens       int64          `json:"limit_tokens"`
	UsedTokens        int64          `json:"used_tokens"`
	RemainingTokens   int64          `json:"remaining_tokens"`
	ResetsAt          time.Time      `json:"resets_at"`
	Enabled           bool           `json:"enabled"`
	Voice             string         `json:"voice"`
	MaxSeconds        int            `json:"max_seconds"`
	Reason            string         `json:"reason"`
	OverlayConfigured bool           `json:"overlay_configured,omitempty"`
}

type VoiceGetRequest struct {
	ActorID           string `json:"actor_id"`
	BroadcasterUserID string `json:"broadcaster_user_id"`
}

type VoiceSetRequest struct {
	VoiceGetRequest
	Enabled    bool   `json:"enabled"`
	Voice      string `json:"voice"`
	MaxSeconds int    `json:"max_seconds"`
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
