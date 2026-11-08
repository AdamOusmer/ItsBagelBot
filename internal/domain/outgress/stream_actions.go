// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

const (
	TypePollCreate       = "poll_create"
	TypePollEnd          = "poll_end"
	TypePredictionCreate = "prediction_create"
	TypePredictionEnd    = "prediction_end"
	TypeRaidStart        = "raid_start"
	TypeRaidCancel       = "raid_cancel"
	TypeModAdd           = "mod_add"
	TypeModRemove        = "mod_remove"
	TypeVIPAdd           = "vip_add"
	TypeVIPRemove        = "vip_remove"
	TypeChatSettings     = "chat_settings"
	TypeClearChat        = "clear_chat"
	TypeAdSnooze         = "ad_snooze"
	TypeWhisper          = "whisper"
	TypeUnbanResolve     = "unban_resolve"
	TypeBrandedContent   = "branded_content"
)

const (
	PollTerminated = "TERMINATED"
	PollArchived   = "ARCHIVED"

	PredictionLocked   = "LOCKED"
	PredictionResolved = "RESOLVED"
	PredictionCanceled = "CANCELED"

	UnbanApproved = "approved"
	UnbanDenied   = "denied"
)

const (
	ChatModeSlow       = "slow"
	ChatModeFollower   = "follower"
	ChatModeEmote      = "emote"
	ChatModeSubscriber = "subscriber"
	ChatModeUnique     = "unique"
)

type PollCreate struct {
	Title    string   `json:"title"`
	Choices  []string `json:"choices"`
	Duration int      `json:"duration"`
}

type PollEnd struct {
	Status string `json:"status"`
}

type PredictionCreate struct {
	Title    string   `json:"title"`
	Outcomes []string `json:"outcomes"`
	Window   int      `json:"window"`
}

type PredictionEnd struct {
	Status string `json:"status"`
	Winner int    `json:"winner,omitempty"`
}

type ChatSettings struct {
	Mode     string `json:"mode"`
	Enabled  bool   `json:"enabled"`
	Duration int    `json:"duration,omitempty"`
}

type Whisper struct {
	Message string `json:"message"`
}

type UnbanResolve struct {
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
}

type BrandedContent struct {
	Enabled bool `json:"enabled"`
}
