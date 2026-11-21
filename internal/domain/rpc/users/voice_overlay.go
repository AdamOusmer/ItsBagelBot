// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import "ItsBagelBot/internal/domain/rpc"

const (
	VoiceOverlayResolveSubject = "rpc.internal.users.voice.overlay.resolve"
	VoiceOverlayCheckSubject   = "rpc.internal.users.voice.overlay.check"
	VoiceOverlayRevokedPrefix  = "controls.tts.revoked."
)

type VoiceOverlayResolveRequest struct {
	TokenHash string `json:"token_hash"`
}

type VoiceOverlayResolveReply struct {
	UserID     uint64   `json:"user_id"`
	Generation int      `json:"generation"`
	Code       rpc.Code `json:"code,omitempty"`
	Message    string   `json:"message,omitempty"`
}

type VoiceOverlayCheckRequest struct {
	UserID     uint64 `json:"user_id"`
	Generation int    `json:"generation"`
}

type VoiceOverlayCheckReply struct {
	Valid bool `json:"valid"`
}

type VoiceOverlayRotateReply struct {
	URL string `json:"url"`
}
