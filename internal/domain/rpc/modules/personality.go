// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

import "ItsBagelBot/internal/domain/rpc"

type FeedBumpRequest struct {
	BroadcasterID uint64 `json:"broadcaster_id,omitempty"`
	Name          string `json:"name,omitempty"`
}

type FeedBumpReply struct {
	Total   uint64 `json:"total"`
	Channel uint64 `json:"channel,omitempty"`
	Rank    uint64 `json:"rank,omitempty"`
	rpc.Refusal
}

type FeedBoardRequest struct {
	Limit         int    `json:"limit,omitempty"`
	BroadcasterID uint64 `json:"broadcaster_id,omitempty"`
}

type FeedBoardReply struct {
	Entries []FeedBoardEntry `json:"entries,omitempty"`
	Total   uint64           `json:"total"`
	Ranked  uint64           `json:"ranked"`
	Channel uint64           `json:"channel,omitempty"`
	Rank    uint64           `json:"rank,omitempty"`
	rpc.Refusal
}

type FeedBoardEntry struct {
	BroadcasterID uint64 `json:"broadcaster_id"`
	Name          string `json:"name,omitempty"`
	Count         uint64 `json:"count"`
}
