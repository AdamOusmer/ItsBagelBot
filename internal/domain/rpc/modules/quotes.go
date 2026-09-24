// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

import "ItsBagelBot/internal/domain/rpc"

type Quote struct {
	Number    uint64 `json:"number"`
	Text      string `json:"text"`
	AddedBy   string `json:"added_by,omitempty"`
	CreatedAt string `json:"created_at"`
}

type QuoteRequest struct {
	UserID    string `json:"user_id"`
	Number    uint64 `json:"number,omitempty"`
	Text      string `json:"text,omitempty"`
	AddedBy   string `json:"added_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type QuoteReply struct {
	Quote  *Quote  `json:"quote,omitempty"`
	Quotes []Quote `json:"quotes,omitempty"`
	Found  bool    `json:"found,omitempty"`
	rpc.Refusal
}
