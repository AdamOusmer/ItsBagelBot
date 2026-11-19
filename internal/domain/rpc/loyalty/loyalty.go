// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package loyaltyrpc

import (
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/rpc"
)

const InvalidateScope = invalidate.ScopeLoyalty

const (
	VerbBalanceCredit   = "balance.credit"
	VerbBalanceSnapshot = "balance.snapshot"
	VerbWatchStatus     = "watch.status"
	VerbFreezeSet       = "freeze.set"
	VerbFreezeRelease   = "freeze.release"
	VerbFreezeGet       = "freeze.get"

	FreezeModHolder        = "mod"
	FreezeRoundPrefix      = "round:"
	RefusalFrozen          = "frozen"
	RefusalHeldByRound     = "held_by_round"
	RefusalStalePin        = "stale_pin"
	RefusalFrozenTransfers = "frozen_transfers"
	RefusalFrozenGames     = "frozen_games"
)

type Request struct {
	UserID           string `json:"user_id"`
	ViewerID         string `json:"viewer_id,omitempty"`
	ViewerLogin      string `json:"viewer_login,omitempty"`
	TargetViewerID   string `json:"target_viewer_id,omitempty"`
	Name             string `json:"name,omitempty"`
	NewName          string `json:"new_name,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Command          string `json:"command,omitempty"`
	Value            int64  `json:"value,omitempty"`
	Key              string `json:"key,omitempty"`
	AccountCreatedAt int64  `json:"account_created_at,omitempty"`
	Won              bool   `json:"won,omitempty"`
	CounterValue     string `json:"counter_value,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Offset           int    `json:"offset,omitempty"`
	Query            string `json:"query,omitempty"`
	Holder           string `json:"holder,omitempty"`
	FreezeID         string `json:"freeze_id,omitempty"`
	Minutes          int    `json:"minutes,omitempty"`
	BlockTransfer    bool   `json:"block_transfer,omitempty"`
	CloseGames       bool   `json:"close_games,omitempty"`
	Live             bool   `json:"live,omitempty"`
}

type Balance struct {
	ViewerID     string `json:"viewer_id"`
	ViewerLogin  string `json:"viewer_login,omitempty"`
	ViewerName   string `json:"viewer_name,omitempty"`
	Points       int64  `json:"points"`
	PointsExact  string `json:"points_exact,omitempty"`
	WatchSeconds uint64 `json:"watch_seconds"`
	Rank         int64  `json:"rank,omitempty"`
}

type Counter struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
	Value int64  `json:"value,string"`
}

type CounterEntry struct {
	ViewerID    string `json:"viewer_id"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	ViewerName  string `json:"viewer_name,omitempty"`
	Command     string `json:"command,omitempty"`
	Value       int64  `json:"value,string"`
}

type CounterRank struct {
	UserID string `json:"user_id"`
	Value  int64  `json:"value,string"`
}

type FreezeStamp struct {
	FreezeID   string `json:"freeze_id,omitempty"`
	FrozenAsOf int64  `json:"frozen_as_of,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type FreezeView struct {
	FreezeStamp
	Holders       []string `json:"holders"`
	BlockTransfer bool     `json:"block_transfer"`
	CloseGames    bool     `json:"close_games"`
}

type Reply struct {
	FreezeStamp
	Freeze        *FreezeView    `json:"freeze,omitempty"`
	Balance       *Balance       `json:"balance,omitempty"`
	TargetBalance *Balance       `json:"target_balance,omitempty"`
	Top           []Balance      `json:"top,omitempty"`
	Total         int64          `json:"total,omitempty"`
	Counter       *Counter       `json:"counter,omitempty"`
	Counters      []Counter      `json:"counters,omitempty"`
	Entries       []CounterEntry `json:"entries,omitempty"`
	Board         []CounterRank  `json:"board,omitempty"`
	Found         bool           `json:"found,omitempty"`
	Spent         bool           `json:"spent,omitempty"`
	Replayed      bool           `json:"replayed,omitempty"`
	LimitExceeded bool           `json:"limit_exceeded,omitempty"`
	rpc.Refusal
}

type BalanceSnapshot struct {
	Points       int64  `json:"points"`
	WatchSeconds uint64 `json:"watch_seconds"`
	Found        bool   `json:"found"`
	FreezeStamp
	rpc.Refusal
}

type WatchStatus struct {
	OldestPendingAgeMS int64 `json:"oldest_pending_age_ms"`
	Healthy            bool  `json:"healthy"`
	rpc.Refusal
}
