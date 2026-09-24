// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package loyaltyrpc

import "ItsBagelBot/internal/domain/rpc"

type Request struct {
	UserID      string `json:"user_id"`
	ViewerID    string `json:"viewer_id,omitempty"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	Name        string `json:"name,omitempty"`
	NewName     string `json:"new_name,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Command     string `json:"command,omitempty"`
	Value       int64  `json:"value,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type Balance struct {
	ViewerID     string `json:"viewer_id"`
	ViewerLogin  string `json:"viewer_login,omitempty"`
	ViewerName   string `json:"viewer_name,omitempty"`
	Points       int64  `json:"points"`
	WatchSeconds uint64 `json:"watch_seconds"`
}

type Counter struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
	Value int64  `json:"value"`
}

type CounterEntry struct {
	ViewerID    string `json:"viewer_id"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	ViewerName  string `json:"viewer_name,omitempty"`
	Command     string `json:"command,omitempty"`
	Value       int64  `json:"value"`
}

type CounterRank struct {
	UserID string `json:"user_id"`
	Value  int64  `json:"value"`
}

type Reply struct {
	Balance       *Balance       `json:"balance,omitempty"`
	TargetBalance *Balance       `json:"target_balance,omitempty"`
	Top           []Balance      `json:"top,omitempty"`
	Counter       *Counter       `json:"counter,omitempty"`
	Counters      []Counter      `json:"counters,omitempty"`
	Entries       []CounterEntry `json:"entries,omitempty"`
	Board         []CounterRank  `json:"board,omitempty"`
	Found         bool           `json:"found,omitempty"`
	Spent         bool           `json:"spent,omitempty"`
	rpc.Refusal
}
