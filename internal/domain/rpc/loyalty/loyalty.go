// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package loyaltyrpc

import (
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/rpc"
)

const InvalidateScope = invalidate.ScopeLoyalty

const CounterTargetRequired = "counter_target_required"

const (
	VerbBalanceCredit       = "balance.credit"
	VerbBalanceSnapshot     = "balance.snapshot"
	VerbBalanceCountAtLeast = "balance.count_at_least"
	VerbWatchStatus         = "watch.status"
	VerbFreezeSet           = "freeze.set"
	VerbFreezeRelease       = "freeze.release"
	VerbFreezeGet           = "freeze.get"

	VerbSnapshotCreate = "snapshot.create"
	VerbSnapshotDrop   = "snapshot.drop"

	RefusalFrozen                = "frozen"
	RefusalStalePin              = "stale_pin"
	RefusalSnapshotGone rpc.Code = "snapshot_gone"
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
	RequireTarget    bool   `json:"require_target,omitempty"`
	Value            int64  `json:"value,omitempty"`
	Key              string `json:"key,omitempty"`
	AccountCreatedAt *int64 `json:"account_created_at,omitempty"`
	Won              bool   `json:"won,omitempty"`
	CounterValue     string `json:"counter_value,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Offset           int    `json:"offset,omitempty"`
	Query            string `json:"query,omitempty"`
	FreezeID         string `json:"freeze_id,omitempty"`
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

type FreezeSetRequest struct {
	UserID  string `json:"user_id"`
	Minutes int    `json:"minutes"`
}

type FreezeReleaseRequest struct {
	UserID string `json:"user_id"`
}

type FreezeGetRequest struct {
	UserID string `json:"user_id"`
}

type FreezeTimes struct {
	FrozenAsOf int64 `json:"frozen_as_of"`
	ExpiresAt  int64 `json:"expires_at"`
	rpc.Refusal
}

type FreezeView struct {
	Active bool `json:"active"`
	FreezeTimes
}

type FreezeReleased struct {
	Released bool `json:"released"`
	rpc.Refusal
}

type SnapshotCreateRequest struct {
	UserID    string `json:"user_id"`
	Owner     string `json:"owner"`
	ExpiresAt int64  `json:"expires_at"`
}

type SnapshotDropRequest struct {
	UserID     string `json:"user_id"`
	SnapshotID string `json:"snapshot_id"`
}

type SnapshotRef struct {
	SnapshotID string `json:"snapshot_id"`
	AsOf       int64  `json:"as_of"`
	rpc.Refusal
}

type SnapshotDropped struct {
	Dropped bool `json:"dropped"`
	rpc.Refusal
}

type BalanceSnapshotRequest struct {
	UserID     string `json:"user_id"`
	ViewerID   string `json:"viewer_id"`
	SnapshotID string `json:"snapshot_id,omitempty"`
}

type Reply struct {
	FreezeStamp
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
	rpc.Refusal
}

type BalanceCountRequest struct {
	SnapshotID      string `json:"snapshot_id,omitempty"`
	UserID          string `json:"user_id"`
	MinPoints       int64  `json:"min_points,omitempty"`
	MinWatchSeconds int64  `json:"min_watch_seconds,omitempty"`
}

type BalanceCount struct {
	Count int64 `json:"count"`
	rpc.Refusal
}

type WatchStatus struct {
	OldestPendingAgeMS int64 `json:"oldest_pending_age_ms"`
	Healthy            bool  `json:"healthy"`
	rpc.Refusal
}
