// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package data

import "strings"

const (
	SubjectLoyaltyEarned   = "data.loyalty.earned"
	SubjectLoyaltyCounters = "data.loyalty.counters"
)

const (
	CounterScopeBot           = "bot"
	CounterScopeChannel       = "channel"
	CounterScopeViewer        = "viewer"
	CounterScopeCommand       = "command"
	CounterScopeViewerCommand = "viewer_command"
)

const (
	CounterMessagesProcessed = "messages_processed"
	CounterEventsProcessed   = "events_processed"
)

const (
	CounterCommandsAnswered = "commands_answered"
	CounterModActionsTaken  = "mod_actions"
)

const (
	TrialCounterPrefix   = "trial_"
	CounterTrialDecoded  = TrialCounterPrefix + "decoded"
	CounterTrialAnswered = TrialCounterPrefix + "answered"
	CounterTrialPromoted = TrialCounterPrefix + "promoted"
)

var systemCounterNames = []string{
	CounterMessagesProcessed,
	CounterEventsProcessed,
	CounterCommandsAnswered,
	CounterModActionsTaken,
}

func SystemCounter(name string) bool {
	if strings.HasPrefix(name, TrialCounterPrefix) {
		return true
	}
	for _, n := range systemCounterNames {
		if n == name {
			return true
		}
	}
	return false
}

func SystemCounterNames() []string {
	return append([]string(nil), systemCounterNames...)
}

type LoyaltyEarnEntry struct {
	ViewerID     uint64 `json:"viewer_id"`
	ViewerLogin  string `json:"viewer_login,omitempty"`
	ViewerName   string `json:"viewer_name,omitempty"`
	Points       int64  `json:"points,omitempty"`
	WatchSeconds uint64 `json:"watch_seconds,omitempty"`
}

type LoyaltyEarnedDTO struct {
	UserID  uint64             `json:"user_id"`
	Entries []LoyaltyEarnEntry `json:"entries"`
}

type CounterBumpEntry struct {
	Name        string `json:"name"`
	Scope       string `json:"scope,omitempty"`
	ViewerID    uint64 `json:"viewer_id,omitempty"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	ViewerName  string `json:"viewer_name,omitempty"`
	Command     string `json:"command,omitempty"`
	Delta       int64  `json:"delta"`
}

type CounterBumpedDTO struct {
	BatchID string             `json:"batch_id,omitempty"`
	UserID  uint64             `json:"user_id"`
	Bumps   []CounterBumpEntry `json:"bumps"`
}

// WatchAwardDTO is immutable, admission-fenced work accepted into the Valkey
// outbox. WindowID and Chunk remain stable across pagination/crash retries.
// It bypasses the loss-tolerant legacy earned accumulator.
type WatchAwardDTO struct {
	WindowStartedAtUnixMilli int64              `json:"window_started_at_unix_milli,omitempty"`
	AccountCreatedAt         int64              `json:"account_created_at"`
	UserID                   uint64             `json:"user_id"`
	Generation               string             `json:"generation"`
	LiveSession              string             `json:"live_session"`
	WindowID                 string             `json:"window_id"`
	Chunk                    uint32             `json:"chunk"`
	Entries                  []LoyaltyEarnEntry `json:"entries"`
}
