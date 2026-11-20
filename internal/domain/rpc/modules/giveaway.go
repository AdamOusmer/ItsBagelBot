// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

import "ItsBagelBot/internal/domain/rpc"

const GiveawaySuffix = ".giveaway"

const (
	VerbGiveawayRecord    = "record"
	VerbGiveawayDeliver   = "deliver"
	VerbGiveawayRecent    = "recent"
	VerbGiveawayWonWithin = "won_within"
)

const (
	GiveawayMaxWinners    = 100
	GiveawayMaxViewerIDs  = 100
	GiveawayMaxRecentPage = 50
	GiveawayRoundIDMaxLen = 36
	GiveawayPrizeMaxLen   = 100
	GiveawayLoginMaxLen   = 64
)

type GiveawayWinner struct {
	RoundID      string `json:"round_id"`
	Slot         int    `json:"slot"`
	ViewerID     string `json:"viewer_id"`
	Login        string `json:"login"`
	Prize        string `json:"prize"`
	WonAt        int64  `json:"won_at"`
	Claimed      bool   `json:"claimed"`
	ClaimedAt    int64  `json:"claimed_at,omitempty"`
	DeliveredAt  int64  `json:"delivered_at,omitempty"`
	DeliveryNote string `json:"delivery_note,omitempty"`
}

type GiveawayRecentQuery struct {
	RoundID string `json:"round_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

type GiveawayRequest struct {
	UserID       string           `json:"user_id"`
	Winners      []GiveawayWinner `json:"winners,omitempty"`
	RoundID      string           `json:"round_id,omitempty"`
	Slot         int              `json:"slot,omitempty"`
	Delivered    bool             `json:"delivered,omitempty"`
	DeliveryNote string           `json:"delivery_note,omitempty"`
	ViewerIDs    []string         `json:"viewer_ids,omitempty"`
	SinceMs      int64            `json:"since_ms,omitempty"`
	Limit        int              `json:"limit,omitempty"`
	Offset       int              `json:"offset,omitempty"`
}

type GiveawayReply struct {
	Winners   []GiveawayWinner `json:"winners,omitempty"`
	ViewerIDs []string         `json:"viewer_ids,omitempty"`
	Found     bool             `json:"found,omitempty"`
	rpc.Refusal
}
