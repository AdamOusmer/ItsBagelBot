// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulesrpc

import "ItsBagelBot/internal/domain/rpc"

type VariableRequest struct {
	UserID   string   `json:"user_id"`
	ViewerID string   `json:"viewer_id,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Name     string   `json:"name,omitempty"`
	Names    []string `json:"names,omitempty"`
	Text     string   `json:"text,omitempty"`
	Value    int64    `json:"value,omitempty"`
}

type VariableReply struct {
	Found         bool              `json:"found,omitempty"`
	LimitExceeded bool              `json:"limit_exceeded,omitempty"`
	Variables     map[string]string `json:"variables,omitempty"`
	rpc.Refusal
}

// CooldownWindow carries TTLSeconds on a claim and ExpiresAtMS on every reply and release.
type CooldownWindow struct {
	Key         string `json:"key"`
	TTLSeconds  int64  `json:"ttl_s,omitempty"`
	ExpiresAtMS int64  `json:"expires_at_ms,omitempty"`
}

type CooldownRequest struct {
	UserID  string           `json:"user_id"`
	Windows []CooldownWindow `json:"windows,omitempty"`
}

// CooldownReply lists the won windows when Claimed, otherwise the window that refused with its expiry.
type CooldownReply struct {
	Claimed bool             `json:"claimed,omitempty"`
	Found   bool             `json:"found,omitempty"`
	Windows []CooldownWindow `json:"windows,omitempty"`
	rpc.Refusal
}
