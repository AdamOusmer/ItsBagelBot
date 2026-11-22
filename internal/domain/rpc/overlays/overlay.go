// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	OverlayListSubject        = "rpc.overlays.overlay.list"
	OverlayAddTemplateSubject = "rpc.overlays.overlay.add_template"
	OverlayGetSubject         = "rpc.overlays.overlay.get"
	OverlaySaveSubject        = "rpc.overlays.overlay.save"
	OverlayRotateSubject      = "rpc.overlays.overlay.rotate"
	OverlayDeleteSubject      = "rpc.overlays.overlay.delete"

	OverlayResolveSubject = "rpc.internal.projector.overlays.resolve"

	OverlayURLPrefix = "https://overlay.itsbagelbot.com/o#"
)

type TokenHash = TtsTokenHash

func HashToken(token string) TokenHash {
	sum := sha256.Sum256([]byte(token))
	return TokenHash(hex.EncodeToString(sum[:]))
}

type Overlay struct {
	ID         uint64          `json:"id"`
	UserID     uint64          `json:"user_id"`
	Kind       string          `json:"kind"`
	Template   string          `json:"template,omitempty"`
	Name       string          `json:"name"`
	TokenHash  TokenHash       `json:"token_hash"`
	Generation int             `json:"generation"`
	Document   json.RawMessage `json:"document,omitempty"`
	Revision   uint64          `json:"revision"`
	CreatedAt  int64           `json:"created_at"`
}

type OverlayUserRequest struct {
	UserID string `json:"user_id"`
}

func (r OverlayUserRequest) Requested() string { return r.UserID }

type OverlayAddTemplateRequest struct {
	OverlayUserRequest
	Template string `json:"template"`
}

type OverlayRefRequest struct {
	OverlayUserRequest
	ID uint64 `json:"id"`
}

type OverlaySaveRequest struct {
	OverlayRefRequest
	Config   PoweredConfig `json:"config"`
	Revision uint64        `json:"revision"`
}

type OverlayView struct {
	ID       uint64          `json:"id"`
	Kind     string          `json:"kind"`
	Template string          `json:"template,omitempty"`
	Name     string          `json:"name"`
	Revision uint64          `json:"revision"`
	Document json.RawMessage `json:"document,omitempty"`
	URL      string          `json:"url,omitempty"`
	rpc.Refusal
}

type OverlayListReply struct {
	Overlays []OverlayView `json:"overlays"`
	rpc.Refusal
}

type OverlayResolveRequest struct {
	TokenHash TokenHash `json:"token_hash"`
}

type OverlayResolveReply struct {
	UserID     uint64          `json:"user_id"`
	OverlayID  uint64          `json:"overlay_id"`
	Kind       string          `json:"kind"`
	Template   string          `json:"template,omitempty"`
	Revision   uint64          `json:"revision"`
	Generation int             `json:"generation"`
	Allowed    bool            `json:"allowed"`
	Document   json.RawMessage `json:"document,omitempty"`
	rpc.Refusal
}
