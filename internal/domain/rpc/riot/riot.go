// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package riotrpc

type Link struct {
	Puuid    string `json:"puuid"`
	GameName string `json:"game_name"`
	TagLine  string `json:"tag_line"`
	Shard    string `json:"shard"`
}

type LinkSetRequest struct {
	UserID string `json:"user_id"`
	Link
}

type LinkUserRequest struct {
	UserID string `json:"user_id"`
}

type LinkReply struct {
	Linked   bool  `json:"linked"`
	LinkedAt int64 `json:"linked_at,omitempty"`
	Link
	Error string `json:"error,omitempty"`
}

type LinkMutateReply struct {
	Error string `json:"error,omitempty"`
}
