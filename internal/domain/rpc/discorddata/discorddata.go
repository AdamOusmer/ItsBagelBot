// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discorddata

import (
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
)

const (
	VerbBindingGet               = "binding.get"
	VerbBindingSet               = "binding.set"
	VerbBindingDelete            = "binding.delete"
	VerbBindingListByBroadcaster = "binding.list_by_broadcaster"

	VerbConfigGet = "config.get"
	VerbConfigSet = "config.set"

	VerbTicketOpen  = "ticket.open"
	VerbTicketClaim = "ticket.claim"
	VerbTicketClose = "ticket.close"
	VerbTicketGet   = "ticket.get"
	VerbTicketCount = "ticket.count"
	VerbTicketList  = "ticket.list"

	VerbTranscriptPut = "transcript.put"
	VerbTranscriptGet = "transcript.get"

	VerbXPGet   = "xp.get"
	VerbXPAdd   = "xp.add"
	VerbXPDaily = "xp.daily"
	VerbXPTop   = "xp.top"
)

const (
	CodeOK       = rpc.CodeOK
	CodeNotFound = rpc.CodeNotFound
	CodeInvalid  = rpc.CodeInvalid
	CodeConflict = rpc.CodeConflict
	CodeInternal = rpc.CodeInternal

	CodeBoundElsewhere rpc.Code = "bound_elsewhere"
	CodeNotBound       rpc.Code = "not_bound"
	CodeLimit          rpc.Code = "limit"
)

const (
	StatusOpen     = "open"
	StatusClaimed  = "claimed"
	StatusClosed   = "closed"
	StatusArchived = "archived"
)

type BindingGetRequest struct {
	GuildID string `json:"guild_id"`
}

type BindingGetReply struct {
	BroadcasterID uint64   `json:"broadcaster_id"`
	Found         bool     `json:"found"`
	Error         string   `json:"error,omitempty"`
	Code          rpc.Code `json:"code,omitempty"`
}

type BindingSetRequest struct {
	GuildID       string `json:"guild_id"`
	BroadcasterID uint64 `json:"broadcaster_id"`
	InstalledBy   string `json:"installed_by,omitempty"`
}

type BindingSetReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code,omitempty"`
}

type BindingDeleteRequest struct {
	GuildID       string `json:"guild_id"`
	BroadcasterID uint64 `json:"broadcaster_id,omitempty"`
}

type BindingDeleteReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code,omitempty"`
}

type BindingListByBroadcasterRequest struct {
	BroadcasterID uint64 `json:"broadcaster_id"`
}

type BindingListByBroadcasterReply struct {
	Guilds []Binding `json:"guilds"`
	Error  string    `json:"error,omitempty"`
	Code   rpc.Code  `json:"code,omitempty"`
}

type Binding struct {
	GuildID       string `json:"guild_id"`
	BoundAtUnixMs int64  `json:"bound_at_unix_ms"`
	InstalledBy   string `json:"installed_by,omitempty"`
}

type ConfigGetRequest struct {
	GuildID string `json:"guild_id"`
}

type ConfigGetReply struct {
	Config  ddiscord.Config `json:"config"`
	Version int             `json:"version"`
	Found   bool            `json:"found"`
	Error   string          `json:"error,omitempty"`
	Code    rpc.Code        `json:"code,omitempty"`
}

type ConfigSetRequest struct {
	GuildID         string          `json:"guild_id"`
	BroadcasterID   uint64          `json:"broadcaster_id"`
	Config          ddiscord.Config `json:"config"`
	ExpectedVersion int             `json:"expected_version"`
}

type ConfigSetReply struct {
	Version int      `json:"version"`
	Fields  []string `json:"fields,omitempty"`
	Error   string   `json:"error,omitempty"`
	Code    rpc.Code `json:"code,omitempty"`
}

type TicketOpenRequest struct {
	GuildID        string `json:"guild_id"`
	ChannelID      string `json:"channel_id"`
	OpenerID       string `json:"opener_id"`
	Subject        string `json:"subject,omitempty"`
	OpenLimit      int    `json:"open_limit,omitempty"`
	PanelMessageID string `json:"panel_message_id,omitempty"`
}

type Refusal = rpc.Refusal

type TicketOpenReply struct {
	TicketID  int `json:"ticket_id"`
	OpenCount int `json:"open_count"`
	Refusal
}

type TicketClaimRequest struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	StaffID   string `json:"staff_id"`
}

type TicketClaimReply struct {
	TicketID int `json:"ticket_id"`
	Refusal
}

type TicketCloseRequest struct {
	GuildID           string `json:"guild_id"`
	ChannelID         string `json:"channel_id"`
	ClosedBy          string `json:"closed_by"`
	ArchivedChannelID string `json:"archived_channel_id,omitempty"`
}

type TicketCloseReply struct {
	TicketID int    `json:"ticket_id"`
	OpenerID string `json:"opener_id"`
	Refusal
}

type TicketGetRequest struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
}

type TicketGetReply struct {
	Ticket Ticket `json:"ticket"`
	Found  bool   `json:"found"`
	Refusal
}

type TicketCountRequest struct {
	GuildID  string `json:"guild_id"`
	OpenerID string `json:"opener_id"`
}

type TicketCountReply struct {
	Count int `json:"count"`
	Refusal
}

type TicketListRequest struct {
	GuildID string `json:"guild_id"`
	Status  string `json:"status,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
}

type TicketListReply struct {
	Tickets    []Ticket `json:"tickets"`
	NextCursor string   `json:"next_cursor,omitempty"`
	Refusal
}

type Ticket struct {
	ID                int    `json:"id"`
	GuildID           string `json:"guild_id"`
	ChannelID         string `json:"channel_id"`
	OpenerID          string `json:"opener_id"`
	Status            string `json:"status"`
	Subject           string `json:"subject,omitempty"`
	ClaimedBy         string `json:"claimed_by,omitempty"`
	ClosedBy          string `json:"closed_by,omitempty"`
	ArchivedChannelID string `json:"archived_channel_id,omitempty"`
	PanelMessageID    string `json:"panel_message_id,omitempty"`
	OpenedAtUnixMs    int64  `json:"opened_at_unix_ms"`
	ClaimedAtUnixMs   int64  `json:"claimed_at_unix_ms,omitempty"`
	ClosedAtUnixMs    int64  `json:"closed_at_unix_ms,omitempty"`
}

type TranscriptPutRequest struct {
	TicketID     int    `json:"ticket_id"`
	Body         string `json:"body"`
	MessageCount int    `json:"message_count"`
}

type TranscriptPutReply struct {
	Refusal
}

type TranscriptGetRequest struct {
	TicketID int `json:"ticket_id"`
}

type TranscriptGetReply struct {
	Body           string `json:"body"`
	MessageCount   int    `json:"message_count"`
	StoredAtUnixMs int64  `json:"stored_at_unix_ms"`
	Found          bool   `json:"found"`
	Refusal
}

type XPGetRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

type XPGetReply struct {
	XPValue         int64    `json:"xp"`
	Level           int      `json:"level"`
	LastDailyUnixMs int64    `json:"last_daily_unix_ms,omitempty"`
	Found           bool     `json:"found"`
	Error           string   `json:"error,omitempty"`
	Code            rpc.Code `json:"code,omitempty"`
}

type XPAddRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	Delta   int64  `json:"delta"`
}

type XPAddReply struct {
	XPValue   int64    `json:"xp"`
	Level     int      `json:"level"`
	LeveledUp bool     `json:"leveled_up"`
	Error     string   `json:"error,omitempty"`
	Code      rpc.Code `json:"code,omitempty"`
}

type XPDailyRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	Amount  int64  `json:"amount"`
}

type XPDailyReply struct {
	Granted    bool     `json:"granted"`
	XPValue    int64    `json:"xp"`
	Level      int      `json:"level"`
	NextUnixMs int64    `json:"next_unix_ms,omitempty"`
	Error      string   `json:"error,omitempty"`
	Code       rpc.Code `json:"code,omitempty"`
}

type XPTopRequest struct {
	GuildID string `json:"guild_id"`
	Limit   int    `json:"limit,omitempty"`
}

type XPTopReply struct {
	Rows  []XPRow  `json:"rows"`
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code,omitempty"`
}

type XPRow struct {
	UserID  string `json:"user_id"`
	XPValue int64  `json:"xp"`
	Level   int    `json:"level"`
}
