// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commandsrpc

import "ItsBagelBot/internal/domain/rpc"

import "ItsBagelBot/internal/domain/rpc/projection"

type DashboardRequest struct {
	UserID           string   `json:"user_id"`
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases"`
	Response         string   `json:"response"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm"`
	Cooldown         uint     `json:"cooldown"`
	AllowedUserID    string   `json:"allowed_user_id"`
	BumpCounter      string   `json:"bump_counter"`
	OriginalName     string   `json:"original_name"`
}

type DashboardReply struct {
	Commands []projection.CommandView `json:"commands"`
	rpc.Refusal
}
