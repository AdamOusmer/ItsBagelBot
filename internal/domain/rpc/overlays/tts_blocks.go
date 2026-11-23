// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import "ItsBagelBot/internal/domain/rpc"

type TtsBlockRequest struct {
	TtsUserRequest
	ViewerID    string `json:"viewer_id"`
	ViewerLogin string `json:"viewer_login,omitempty"`
}

type TtsViewerBlock struct {
	ViewerID    string `json:"viewer_id"`
	ViewerLogin string `json:"viewer_login,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

type TtsBlocksReply struct {
	Viewers []TtsViewerBlock `json:"viewers"`
	rpc.Refusal
}
