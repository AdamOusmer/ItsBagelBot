// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordoutgress

import (
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
)

type TicketOpenRequest struct {
	GuildID    string                           `json:"guild_id"`
	Name       string                           `json:"name"`
	ParentID   string                           `json:"parent_id,omitempty"`
	Overwrites []discordapi.PermissionOverwrite `json:"overwrites,omitempty"`
	Content    string                           `json:"content,omitempty"`
	Embed      ddiscord.Embed                   `json:"embed"`
	Buttons    []ddiscord.ButtonSpec            `json:"buttons,omitempty"`
}

type TicketOpenReply struct {
	ChannelID string   `json:"channel_id,omitempty"`
	MessageID string   `json:"message_id,omitempty"`
	Error     string   `json:"error,omitempty"`
	Code      rpc.Code `json:"code,omitempty"`
}

type TicketClaimRequest struct {
	ChannelID string         `json:"channel_id"`
	MessageID string         `json:"message_id"`
	Content   string         `json:"content,omitempty"`
	Embed     ddiscord.Embed `json:"embed"`
	Note      string         `json:"note,omitempty"`
}

type TicketClaimReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code,omitempty"`
}

type TicketCloseSummary struct {
	Opener         string `json:"opener,omitempty"`
	Closer         string `json:"closer,omitempty"`
	OpenedAtUnixMs int64  `json:"opened_at_unix_ms,omitempty"`
}

type TicketCloseRequest struct {
	GuildID           string             `json:"guild_id"`
	ChannelID         string             `json:"channel_id"`
	TicketID          int                `json:"ticket_id,omitempty"`
	ChannelName       string             `json:"channel_name,omitempty"`
	OpenerID          string             `json:"opener_id,omitempty"`
	Transcript        bool               `json:"transcript,omitempty"`
	LogChannelID      string             `json:"log_channel_id,omitempty"`
	ArchiveCategoryID string             `json:"archive_category_id,omitempty"`
	StaffRoleIDs      []string           `json:"staff_role_ids,omitempty"`
	Summary           TicketCloseSummary `json:"summary"`
}

type TicketCloseReply struct {
	MessageCount      int      `json:"message_count,omitempty"`
	TranscriptBody    string   `json:"transcript_body,omitempty"`
	Truncated         bool     `json:"truncated,omitempty"`
	ArchivedChannelID string   `json:"archived_channel_id,omitempty"`
	Error             string   `json:"error,omitempty"`
	Code              rpc.Code `json:"code,omitempty"`
}

type TicketMemberAddRequest struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
}

type TicketMemberAddReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code,omitempty"`
}

type TicketPanelRequest struct {
	GuildID   string                `json:"guild_id"`
	ChannelID string                `json:"channel_id"`
	Content   string                `json:"content,omitempty"`
	Embed     ddiscord.Embed        `json:"embed"`
	Buttons   []ddiscord.ButtonSpec `json:"buttons,omitempty"`
}

type TicketPanelReply struct {
	MessageID string   `json:"message_id,omitempty"`
	Error     string   `json:"error,omitempty"`
	Code      rpc.Code `json:"code,omitempty"`
}
