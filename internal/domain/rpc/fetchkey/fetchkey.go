// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package fetchkey holds the shared wire types for the commands service's
// $(urlfetch) RPC surface: the internal decrypt verb gossip calls once per
// fetch, and the dashboard verbs the console drives. Keys are broadcaster
// secrets: sealed at rest with the commands service's own Tink keyset, never
// echoed back over any dashboard verb, and handed to gossip decrypted exactly
// once per upstream call — never cached, never projected.
package fetchkey

import "time"

// KeyGetRequest is the internal decrypt request gossip makes before dialing a
// user-defined endpoint.
type KeyGetRequest struct {
	UserID string `json:"user_id"`
	Label  string `json:"label"`
}

// KeyGetReply carries the decrypted key or a terminal error. Empty Key with
// empty Error means no key is on file for that label (the caller treats that
// as "definition runs keyless", not a failure).
type KeyGetReply struct {
	Key   string `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}

// FetchView is the canonical wire shape of one definition as stored in the
// Valkey projection field "fetch:<name>". Field set and json tags match
// internal/projection's view exactly so consumers decode without conversion.
// It deliberately carries key_label only — sealed material never enters the
// projection or any cache.
type FetchView struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	JSONPath []string `json:"json_path,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"`
	IsActive bool     `json:"is_active"`
}

// KeyView is the custody listing row: label plus last4 only. The plaintext
// value has no read path anywhere in the fleet.
type KeyView struct {
	Label     string    `json:"label"`
	Last4     string    `json:"last4"`
	CreatedAt time.Time `json:"created_at"`
}

// FetchListRequest asks for one broadcaster's definitions and keys (metadata
// only; values never appear).
type FetchListRequest struct {
	UserID string `json:"user_id"`
}

// FetchListReply carries both lists; either may be empty without being an
// error. UserID echoes the request (the projection fallback verb convention).
type FetchListReply struct {
	UserID  string      `json:"user_id,omitempty"`
	Fetches []FetchView `json:"fetches"`
	Keys    []KeyView   `json:"keys"`
	Error   string      `json:"error,omitempty"`
}

// FetchDefSetRequest upserts one definition. OriginalName, when set and
// different from Name, makes the write a rename of that existing row (the
// commands upsert convention). JSONPath segments arrive pre-split on dots.
type FetchDefSetRequest struct {
	UserID       string   `json:"user_id"`
	Name         string   `json:"name"`
	OriginalName string   `json:"original_name,omitempty"`
	URL          string   `json:"url"`
	JSONPath     []string `json:"json_path,omitempty"`
	KeyLabel     string   `json:"key_label,omitempty"`
	IsActive     bool     `json:"is_active"`
}

// FetchKeySetRequest stores (or rotates) one sealed API key against a label.
// The Value rides this request exactly once and is never returned by anything.
type FetchKeySetRequest struct {
	UserID string `json:"user_id"`
	Label  string `json:"label"`
	Value  string `json:"value"`
}

// FetchKeySetReply acks with the last4 derived from the just-sealed value so
// the console can confirm which credential is stored without any decrypt.
type FetchKeySetReply struct {
	Last4 string `json:"last4,omitempty"`
	Error string `json:"error,omitempty"`
}

// FetchDeleteRequest removes one object: Kind "def" deletes the named
// definition (refused while commands still reference it, unless Force), Kind
// "key" deletes the labeled key — always allowed; dangling key_labels fail
// closed at fetch time until relinked.
type FetchDeleteRequest struct {
	UserID string `json:"user_id"`
	Kind   string `json:"kind"` // "def" | "key"
	Name   string `json:"name,omitempty"`
	Label  string `json:"label,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

// FetchMutateReply is the bare ack envelope for def mutations and deletes.
type FetchMutateReply struct {
	Error string `json:"error,omitempty"`
}
