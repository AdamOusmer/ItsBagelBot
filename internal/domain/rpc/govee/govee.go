// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package goveerpc

type KeySetRequest struct {
	UserID string `json:"user_id"`
	Key    string `json:"key"`
}

type KeyClearRequest struct {
	UserID string `json:"user_id"`
}

type KeyStatusRequest struct {
	UserID string `json:"user_id"`
}

type KeyStatusReply struct {
	Present bool   `json:"present"`
	Error   string `json:"error,omitempty"`
}

type KeyMutateReply struct {
	Error string `json:"error,omitempty"`
}

type KeyGetRequest struct {
	UserID string `json:"user_id"`
}

type KeyGetReply struct {
	Key   string `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}
