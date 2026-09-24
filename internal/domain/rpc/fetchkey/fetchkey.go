// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fetchkey

import "ItsBagelBot/internal/domain/rpc"

import "time"

type KeyGetRequest struct {
	UserID string `json:"user_id"`
	Label  string `json:"label"`
}

type KeyGetReply struct {
	Key string `json:"key,omitempty"`
	rpc.Refusal
}

type FetchView struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	JSONPath []string `json:"json_path,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"`
	IsActive bool     `json:"is_active"`
}

type KeyView struct {
	Label     string    `json:"label"`
	Last4     string    `json:"last4"`
	CreatedAt time.Time `json:"created_at"`
}

type FetchListRequest struct {
	UserID string `json:"user_id"`
}

type FetchListReply struct {
	UserID  string      `json:"user_id,omitempty"`
	Fetches []FetchView `json:"fetches"`
	Keys    []KeyView   `json:"keys"`
	rpc.Refusal
}

func (r FetchListRequest) Requested() string { return r.UserID }

type FetchDefSetRequest struct {
	UserID       string   `json:"user_id"`
	Name         string   `json:"name"`
	OriginalName string   `json:"original_name,omitempty"`
	URL          string   `json:"url"`
	JSONPath     []string `json:"json_path,omitempty"`
	KeyLabel     string   `json:"key_label,omitempty"`
	IsActive     bool     `json:"is_active"`
}

type FetchKeySetRequest struct {
	UserID string `json:"user_id"`
	Label  string `json:"label"`
	Value  string `json:"value"`
}

type FetchKeySetReply struct {
	Last4 string `json:"last4,omitempty"`
	rpc.Refusal
}

type FetchDeleteRequest struct {
	UserID string `json:"user_id"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Label  string `json:"label,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

type FetchMutateReply struct {
	rpc.Refusal
}
