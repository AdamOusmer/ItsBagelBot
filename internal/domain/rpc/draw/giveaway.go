// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package drawrpc

import "ItsBagelBot/internal/domain/rpc"

const MaxWinners = 100

const GiveawayPrefix = "rpc.draw.giveaway"

const (
	VerbGiveawayLive          = "live"
	VerbGiveawayStart         = "start"
	VerbGiveawayClose         = "close"
	VerbGiveawayExtend        = "extend"
	VerbGiveawayCancel        = "cancel"
	VerbGiveawayReroll        = "reroll"
	VerbGiveawayEndSlots      = "end_slots"
	VerbGiveawaySkipRule      = "skip_rule"
	VerbGiveawayMarkDelivered = "mark_delivered"
	VerbGiveawayEligibleCount = "eligible_count"
)

type GiveawayRequest struct {
	UserID        string `json:"user_id"`
	Source        string `json:"source"`
	ActorID       string `json:"actor_id,omitempty"`
	Preset        string `json:"preset,omitempty"`
	Prize         string `json:"prize,omitempty"`
	Winners       int    `json:"winners,omitempty"`
	WindowSeconds int    `json:"window_seconds,omitempty"`
	RoundID       string `json:"round_id,omitempty"`
	Slot          int    `json:"slot,omitempty"`
	Delivered     *bool  `json:"delivered,omitempty"`
}

func (r GiveawayRequest) Requested() string { return r.UserID }

type GiveawayCandidate struct {
	ViewerID   string            `json:"viewer_id"`
	Login      string            `json:"login,omitempty"`
	Generation int64             `json:"generation"`
	Reason     string            `json:"reason"`
	Detail     map[string]string `json:"detail,omitempty"`
}

type GiveawaySlot struct {
	N           int                 `json:"n"`
	Generation  int64               `json:"generation"`
	ViewerID    string              `json:"viewer_id,omitempty"`
	Login       string              `json:"login,omitempty"`
	State       string              `json:"state"`
	DeadlineMS  int64               `json:"deadline_ms,omitempty"`
	CanSkipRule bool                `json:"can_skip_rule,omitempty"`
	Rejected    []GiveawayCandidate `json:"rejected,omitempty"`
}

type GiveawayRound struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Phase        string         `json:"phase"`
	Prize        string         `json:"prize"`
	Winners      int            `json:"winners"`
	OpenedMS     int64          `json:"opened_ms"`
	ClosesMS     int64          `json:"closes_ms"`
	FreezeEndsMS int64          `json:"freeze_ends_ms,omitempty"`
	Entries      int            `json:"entries"`
	Refused      int64          `json:"refused"`
	Slots        []GiveawaySlot `json:"slots"`
	Requirements []string       `json:"requirements"`
}

type GiveawayReply struct {
	Rounds     *[]GiveawayRound `json:"rounds,omitempty"`
	Round      *GiveawayRound   `json:"round,omitempty"`
	Done       bool             `json:"done,omitempty"`
	Affected   int              `json:"affected,omitempty"`
	Count      *int64           `json:"count,omitempty"`
	FreezeID   string           `json:"freeze_id,omitempty"`
	FrozenAsOf int64            `json:"frozen_as_of,omitempty"`
	ExpiresAt  int64            `json:"expires_at,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	rpc.Refusal
}
