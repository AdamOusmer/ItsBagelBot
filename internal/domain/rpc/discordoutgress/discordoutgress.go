// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordoutgress

import (
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

type ChannelCreateRequest struct {
	GuildID    string                           `json:"guild_id"`
	Name       string                           `json:"name"`
	Type       int                              `json:"type"`
	ParentID   string                           `json:"parent_id,omitempty"`
	Topic      string                           `json:"topic,omitempty"`
	Overwrites []discordapi.PermissionOverwrite `json:"overwrites,omitempty"`
}

type ChannelCreateReply struct {
	ChannelID string `json:"channel_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ChannelDeleteRequest struct {
	ChannelID string `json:"channel_id"`
}

type ChannelDeleteReply struct {
	Error string `json:"error,omitempty"`
}

type ChannelModifyRequest struct {
	ChannelID  string                           `json:"channel_id"`
	Name       string                           `json:"name,omitempty"`
	UserLimit  int                              `json:"user_limit,omitempty"`
	Overwrites []discordapi.PermissionOverwrite `json:"overwrites,omitempty"`
}

type ChannelModifyReply struct {
	Error string `json:"error,omitempty"`
}

type MemberMoveRequest struct {
	GuildID   string `json:"guild_id"`
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
}

type MemberMoveReply struct {
	Error string `json:"error,omitempty"`
}

type PurgeRequest struct {
	ChannelID string `json:"channel_id"`
	Count     int    `json:"count"`
}

type PurgeReply struct {
	Deleted int    `json:"deleted,omitempty"`
	Error   string `json:"error,omitempty"`
}

type LiveOnlineRequest struct {
	GuildID   string         `json:"guild_id"`
	ChannelID string         `json:"channel_id"`
	Embed     ddiscord.Embed `json:"embed"`
}

type LiveOnlineReply struct {
	Error string `json:"error,omitempty"`
}

type LiveOfflineRequest struct {
	GuildID string `json:"guild_id"`
}

type LiveOfflineReply struct {
	Error string `json:"error,omitempty"`
}

type InviteResolveRequest struct {
	Code string `json:"code"`
}

type InviteResolveReply struct {
	GuildID  string `json:"guild_id,omitempty"`
	NotFound bool   `json:"not_found,omitempty"`
	Error    string `json:"error,omitempty"`
}
