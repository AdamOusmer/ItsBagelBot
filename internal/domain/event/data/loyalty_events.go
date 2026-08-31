// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package data

// Loyalty subjects carry summed deltas from the worker to the loyalty service.
// Like SubjectCommandUsed they are loss-tolerant counters: the worker
// aggregates ticks locally and publishes one summed event per flush window, the
// loyalty service folds them into its rows on its own batch flush. A dropped
// event costs at most one window of deltas, never a corrupt balance.
const (
	SubjectLoyaltyEarned   = "data.loyalty.earned"
	SubjectLoyaltyCounters = "data.loyalty.counters"
)

// Counter scopes — the ways a counter can be made. All but bot are per
// channel: a channel counter is one global value per (broadcaster, name); a
// viewer counter tracks one value per (broadcaster, name, viewer); a command
// counter tracks one pooled value per (broadcaster, name, command) across all
// viewers; a viewer+command counter tracks one value per (broadcaster, name,
// command, viewer), so the same counter separates each command's per-viewer
// tally. A bot counter is one value shared across every channel, stored under
// the reserved broadcaster id 0 — created and bumped only by admin/system
// paths, never reachable from broadcaster templates or chat.
const (
	CounterScopeBot           = "bot"
	CounterScopeChannel       = "channel"
	CounterScopeViewer        = "viewer"
	CounterScopeCommand       = "command"
	CounterScopeViewerCommand = "viewer_command"
)

// The two counter names sesame writes for everyone: once fleet-wide in the bot
// namespace, and once per channel behind the public stats boards. They are
// system-owned — the loyalty service refuses to create, set, rename or delete
// them for a broadcaster, and keeps them out of the dashboard's counter list —
// so a channel cannot edit its own standing on a public leaderboard.
const (
	CounterMessagesProcessed = "messages_processed"
	CounterEventsProcessed   = "events_processed"
)

// CounterCommandsAnswered and CounterModActionsTaken are reserved the same
// way as the pair above, for the Overview's per-stream counter panel (see
// internal/projection/valkey.go's StreamCounters and
// console/dashboard/src/lib/server/stream-counters.ts). Neither has a writer
// yet — sesame's bot_stats.go only flushes messages/events per channel today
// — but reserving the names now means the eventual writer needs no protocol
// change, and until it ships an uncreated counter reads as an honest 0
// through counter.get's found:false (same precedent as messages_processed
// before its own writer existed).
const (
	CounterCommandsAnswered = "commands_answered"
	CounterModActionsTaken  = "mod_actions"
)

// SystemCounter reports whether a counter name belongs to the fleet stats set
// above rather than to the broadcaster whose row holds it.
func SystemCounter(name string) bool {
	switch name {
	case CounterMessagesProcessed, CounterEventsProcessed, CounterCommandsAnswered, CounterModActionsTaken:
		return true
	default:
		return false
	}
}

// LoyaltyEarnEntry is one viewer's summed accrual inside a flush window:
// points from subs/cheers/watch ticks plus the watch seconds the window
// covered. Login/name are carried when the source event knew them so the
// service can keep a display identity without ever resolving ids itself.
type LoyaltyEarnEntry struct {
	ViewerID     uint64 `json:"viewer_id"`
	ViewerLogin  string `json:"viewer_login,omitempty"`
	ViewerName   string `json:"viewer_name,omitempty"`
	Points       int64  `json:"points,omitempty"`
	WatchSeconds uint64 `json:"watch_seconds,omitempty"`
}

// LoyaltyEarnedDTO reports one broadcaster's accruals for a flush window. The
// worker chunks large windows (a big channel's watch tick) into multiple
// events so a single publish never approaches the broker's payload ceiling.
type LoyaltyEarnedDTO struct {
	UserID  uint64             `json:"user_id"`
	Entries []LoyaltyEarnEntry `json:"entries"`
}

// CounterBumpEntry is one counter's summed delta inside a flush window. Scope
// travels with the bump so the service can create the counter row on first
// use; on an existing counter the stored scope wins. ViewerID is set only for
// viewer / viewer+command bumps; Command only for command / viewer+command
// bumps — the source's name, i.e. the command's canonical trigger or the
// channel-point reward's title (unique per channel, so it names the reward
// the way a trigger names a command). Empty means a source with no name of
// its own; those land in the counter's empty bucket. A bot-scope bump carries
// neither and rides a DTO with UserID 0 (the reserved bot namespace).
type CounterBumpEntry struct {
	Name     string `json:"name"`
	Scope    string `json:"scope,omitempty"` // CounterScopeChannel when empty
	ViewerID uint64 `json:"viewer_id,omitempty"`
	// Display identity of the bumping viewer, carried when the source event
	// knew it (viewer scopes only) so the service can store a readable name
	// next to the bucket without ever resolving ids itself.
	ViewerLogin string `json:"viewer_login,omitempty"`
	ViewerName  string `json:"viewer_name,omitempty"`
	Command     string `json:"command,omitempty"`
	Delta       int64  `json:"delta"`
}

// CounterBumpedDTO reports one broadcaster's counter deltas for a flush window.
type CounterBumpedDTO struct {
	UserID uint64             `json:"user_id"`
	Bumps  []CounterBumpEntry `json:"bumps"`
}
