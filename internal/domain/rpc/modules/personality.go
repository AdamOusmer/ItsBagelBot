// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

// FeedBumpRequest asks the modules service to record one "feed the bagel"
// (bagel.rpc.modules.personality.feed): always on the permanent fleet-wide
// counter, and on the feeding channel's own row when the caller names one. A
// zero BroadcasterID bumps the fleet counter only.
type FeedBumpRequest struct {
	BroadcasterID uint64 `json:"broadcaster_id,omitempty"`
	// Name is the channel's display name at this feeding; it is stored so a
	// leaderboard line can name the channel without a users-service lookup.
	// Empty leaves the stored name alone.
	Name string `json:"name,omitempty"`
}

// FeedBumpReply returns the lifetime totals after the bump: fleet-wide, this
// channel's own, and the channel's standing (1 = fed the most). Channel and
// Rank are 0 when the request named no broadcaster.
type FeedBumpReply struct {
	Total   uint64 `json:"total"`
	Channel uint64 `json:"channel,omitempty"`
	Rank    uint64 `json:"rank,omitempty"`
	Error   string `json:"error,omitempty"`
}

// FeedBoardRequest reads the feed leaderboard
// (bagel.rpc.modules.personality.feed.board) without feeding anything. Limit
// caps the returned entries; 0 means every ranked channel and a negative limit
// asks for no entries at all (the !bagels command, which wants a standing and
// nothing else). A non-zero BroadcasterID also asks for that channel's own
// standing, which may sit below the returned entries.
type FeedBoardRequest struct {
	Limit         int    `json:"limit,omitempty"`
	BroadcasterID uint64 `json:"broadcaster_id,omitempty"`
}

// FeedBoardReply is the leaderboard, highest count first, plus how many
// channels are ranked in total and the asking channel's own place in them.
type FeedBoardReply struct {
	Entries []FeedBoardEntry `json:"entries,omitempty"`
	// Total is the fleet-wide lifetime count (one bagel, every channel feeds
	// it) — the same number a feeding returns, read without feeding anything.
	Total   uint64 `json:"total"`
	Ranked  uint64 `json:"ranked"`
	Channel uint64 `json:"channel,omitempty"`
	Rank    uint64 `json:"rank,omitempty"`
	Error   string `json:"error,omitempty"`
}

// FeedBoardEntry is one channel's place on the feed leaderboard.
type FeedBoardEntry struct {
	BroadcasterID uint64 `json:"broadcaster_id"`
	Name          string `json:"name,omitempty"`
	Count         uint64 `json:"count"`
}
