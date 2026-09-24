// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import "ItsBagelBot/internal/domain/rpc"

import "ItsBagelBot/pkg/codec"

type Request struct {
	UserID string `json:"user_id"`
}

type CommandView struct {
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm"`
	Cooldown         uint     `json:"cooldown"`
	AllowedUserID    string   `json:"allowed_user_id,omitempty"`
	Uses             uint64   `json:"uses,omitempty"`
	BumpCounter      string   `json:"bump_counter,omitempty"`
}

type ModuleView struct {
	Name      string           `json:"name"`
	IsEnabled bool             `json:"is_enabled"`
	Configs   codec.RawMessage `json:"configs,omitempty"`
	Revision  int              `json:"revision,omitempty"`
}

// internal/projection.Client decodes this into projection.User; keep the tags in sync.
type UserReply struct {
	UserID             string `json:"user_id"`
	Status             string `json:"status"`
	IsActive           bool   `json:"is_active"`
	Banned             bool   `json:"banned"`
	Locale             string `json:"locale,omitempty"`
	CommandsPageHidden bool   `json:"commands_page_hidden,omitempty"`
	rpc.Refusal
}

type CommandsReply struct {
	UserID   string        `json:"user_id"`
	Commands []CommandView `json:"commands"`
	rpc.Refusal
}

type ModulesReply struct {
	UserID  string       `json:"user_id"`
	Modules []ModuleView `json:"modules"`
	rpc.Refusal
}
