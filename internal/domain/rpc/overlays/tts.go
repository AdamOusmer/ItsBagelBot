// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/voiceplan"
)

const (
	TtsGetSubject         = "rpc.overlays.tts.get"
	TtsSaveSubject        = "rpc.overlays.tts.save"
	TtsRotateSubject      = "rpc.overlays.tts.rotate"
	OwnerTtsByUserSubject = "rpc.internal.overlays.tts.by_user"
	OwnerTtsByHashSubject = "rpc.internal.overlays.tts.by_hash"
	TtsResolveSubject     = "rpc.internal.projector.overlays.tts.resolve"
	TtsCheckSubject       = "rpc.internal.projector.overlays.tts.check"
	TtsSettingsSubject    = "rpc.internal.projector.overlays.tts.settings"
	TtsBlocksSubject      = "rpc.overlays.tts.blocks"
	TtsBlockSubject       = "rpc.overlays.tts.block"
	TtsUnblockSubject     = "rpc.overlays.tts.unblock"

	TtsRevokedPrefix = "controls.tts.revoked."
	TtsURLPrefix     = "https://overlay.itsbagelbot.com/tts#"
)

type TtsSettings struct {
	Enabled    bool   `json:"enabled"`
	Voice      string `json:"voice"`
	MaxSeconds int    `json:"max_seconds"`
}

func DefaultTtsSettings() TtsSettings {
	return TtsSettings{Enabled: true, MaxSeconds: voiceplan.MaxClipSeconds}
}

func (s TtsSettings) Validate() error {
	if s.Voice != "" && !voiceplan.ValidVoice(s.Voice) {
		return errors.New("invalid voice")
	}
	if s.MaxSeconds < voiceplan.MinClipSeconds || s.MaxSeconds > voiceplan.MaxClipSeconds {
		return errors.New("invalid voice clip length")
	}
	return nil
}

type TtsTokenHash string

func (h TtsTokenHash) Valid() bool {
	value := string(h)
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type TtsOverlay struct {
	UserID     uint64       `json:"user_id"`
	Settings   TtsSettings  `json:"settings"`
	TokenHash  TtsTokenHash `json:"token_hash,omitempty"`
	Generation int          `json:"generation"`
	Revision   uint64       `json:"revision"`
	CreatedAt  int64        `json:"created_at"`

	BlockedViewers []string `json:"blocked_viewers,omitempty"`
}

func (o TtsOverlay) Configured() bool { return o.TokenHash != "" }

type TtsUserRequest struct {
	UserID string `json:"user_id"`
}

func (r TtsUserRequest) Requested() string { return r.UserID }

type TtsSaveRequest struct {
	TtsUserRequest
	Settings TtsSettings `json:"settings"`
	Revision uint64      `json:"revision"`
}

type TtsSettingsReply struct {
	Settings       TtsSettings `json:"settings"`
	Configured     bool        `json:"configured"`
	BlockedViewers []string    `json:"blocked_viewers,omitempty"`
	rpc.Refusal
}

type TtsView struct {
	TtsSettingsReply
	Revision uint64 `json:"revision"`
	URL      string `json:"url,omitempty"`
}

type OwnerTtsReply struct {
	Overlay *TtsOverlay `json:"overlay,omitempty"`
	rpc.Refusal
}

type TtsResolveRequest struct {
	TokenHash TtsTokenHash `json:"token_hash"`
}

type TtsResolveReply struct {
	UserID     uint64 `json:"user_id"`
	Generation int    `json:"generation"`
	rpc.Refusal
}

type TtsCheckRequest struct {
	UserID     uint64 `json:"user_id"`
	Generation int    `json:"generation"`
}

type TtsCheckReply struct {
	Valid bool `json:"valid"`
}
