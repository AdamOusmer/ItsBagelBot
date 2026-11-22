// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
)

const VoiceExportSubject = "rpc.internal.users.voice.export"

type VoiceExportRequest struct {
	AfterID uint64 `json:"after_id"`
}

type VoiceExportReply struct {
	Overlays []overlaysrpc.TtsOverlay `json:"overlays"`
	NextID   uint64                   `json:"next_id"`
	rpc.Refusal
}
