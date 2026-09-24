// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import (
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
)

const (
	CodeOK        = rpc.CodeOK
	CodeForbidden = rpc.CodeForbidden
	CodeInvalid   = rpc.CodeInvalid
	CodeNotFound  = rpc.CodeNotFound
	CodeConflict  = rpc.CodeConflict

	CodeBoundElsewhere     rpc.Code = "bound_elsewhere"
	CodeNotBound           rpc.Code = "not_bound"
	CodeDiscordUnavailable rpc.Code = "discord_unavailable"
	CodeRateLimited        rpc.Code = "rate_limited"
	CodeTimeout            rpc.Code = "timeout"
	CodeUnknown            rpc.Code = "unknown"
)

type DiscordSetupRequest struct {
	UserID      string            `json:"user_id"`
	GuildID     string            `json:"guild_id"`
	Subscribers bool              `json:"subscribers,omitempty"`
	PinnedRoles map[string]string `json:"pinned_roles,omitempty"`
	InstalledBy string            `json:"installed_by,omitempty"`
}

type DiscordSetupReply struct {
	GuildID                 string   `json:"guild_id,omitempty"`
	LiveChannelID           string   `json:"live_channel_id,omitempty"`
	ClipsChannelID          string   `json:"clips_channel_id,omitempty"`
	WelcomeChannelID        string   `json:"welcome_channel_id,omitempty"`
	VoiceHubID              string   `json:"voice_hub_id,omitempty"`
	LogChannelID            string   `json:"log_channel_id,omitempty"`
	TicketChannelID         string   `json:"ticket_channel_id,omitempty"`
	TicketCategoryID        string   `json:"ticket_category_id,omitempty"`
	TicketArchiveCategoryID string   `json:"ticket_archive_category_id,omitempty"`
	SubsChannelID           string   `json:"subs_channel_id,omitempty"`
	SubsCategoryID          string   `json:"subs_category_id,omitempty"`
	VIPChannelID            string   `json:"vip_channel_id,omitempty"`
	VIPCategoryID           string   `json:"vip_category_id,omitempty"`
	OwnerRoleID             string   `json:"owner_role_id,omitempty"`
	LeadModRoleID           string   `json:"lead_mod_role_id,omitempty"`
	ModsRoleID              string   `json:"mods_role_id,omitempty"`
	VIPRoleID               string   `json:"vip_role_id,omitempty"`
	SubscriberRoleID        string   `json:"subscriber_role_id,omitempty"`
	RegularsRoleID          string   `json:"regulars_role_id,omitempty"`
	MemberRoleID            string   `json:"member_role_id,omitempty"`
	Refused                 string   `json:"refused,omitempty"`
	DroppedPins             []string `json:"dropped_pins,omitempty"`
	Fields                  []string `json:"fields,omitempty"`
	Error                   string   `json:"error,omitempty"`
	Code                    rpc.Code `json:"code"`
}

type DiscordLayoutRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

type DiscordLayoutEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type,omitempty"`
}

type DiscordGuildInfo struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	IconURL     string `json:"icon_url,omitempty"`
	MemberCount int    `json:"member_count,omitempty"`
}

type DiscordLayoutReply struct {
	Channels       []DiscordLayoutEntry `json:"channels,omitempty"`
	Roles          []DiscordLayoutEntry `json:"roles,omitempty"`
	Categories     []DiscordLayoutEntry `json:"categories,omitempty"`
	Guild          *DiscordGuildInfo    `json:"guild,omitempty"`
	BotOnline      bool                 `json:"bot_online,omitempty"`
	BotSinceUnixMS int64                `json:"bot_since_unix_ms,omitempty"`
	LastCloseCode  int                  `json:"last_close_code,omitempty"`
	NeedsReauth    bool                 `json:"needs_reauth,omitempty"`
	Error          string               `json:"error,omitempty"`
	Code           rpc.Code             `json:"code"`
}

type DiscordUnbindRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

type DiscordUnbindReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code"`
}

type DiscordPostRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

type DiscordPostReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code"`
}

type DiscordStatusRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

type DiscordStatusReply struct {
	Online           bool   `json:"online,omitempty"`
	SinceUnixMS      int64  `json:"since_unix_ms,omitempty"`
	SessionResumes   int    `json:"session_resumes,omitempty"`
	GuildPresent     bool   `json:"guild_present,omitempty"`
	GuildName        string `json:"guild_name,omitempty"`
	IconURL          string `json:"icon_url,omitempty"`
	MemberCount      int    `json:"member_count,omitempty"`
	NeedsReauth      bool   `json:"needs_reauth,omitempty"`
	LastCloseCode    int    `json:"last_close_code,omitempty"`
	Flapping         bool   `json:"flapping,omitempty"`
	ConnectsInWindow int    `json:"connects_in_window,omitempty"`
	AtCeiling        bool   `json:"at_ceiling,omitempty"`
	ParkUntilUnixMS  int64  `json:"park_until_unix_ms,omitempty"`

	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code"`
}

type DiscordConfigGetRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

type DiscordConfigGetReply struct {
	Config  ddiscord.Config `json:"config"`
	Version int             `json:"version"`
	Found   bool            `json:"found"`
	Code    rpc.Code        `json:"code"`
	Error   string          `json:"error,omitempty"`
}

type DiscordConfigSetRequest struct {
	UserID          string          `json:"user_id"`
	GuildID         string          `json:"guild_id"`
	Config          ddiscord.Config `json:"config"`
	ExpectedVersion int             `json:"expected_version"`
}

type DiscordConfigSetReply struct {
	Version int      `json:"version"`
	Fields  []string `json:"fields,omitempty"`
	Code    rpc.Code `json:"code"`
	Error   string   `json:"error,omitempty"`
}

type DiscordGuildsListRequest struct {
	UserID string `json:"user_id"`
}

type DiscordGuildsListReply struct {
	Guilds    []DiscordGuildEntry `json:"guilds"`
	Truncated bool                `json:"truncated,omitempty"`
	Code      rpc.Code            `json:"code"`
	Error     string              `json:"error,omitempty"`
}

type DiscordGuildEntry struct {
	GuildID       string `json:"guild_id"`
	Name          string `json:"name,omitempty"`
	IconURL       string `json:"icon_url,omitempty"`
	MemberCount   int    `json:"member_count,omitempty"`
	BotPresent    bool   `json:"bot_present"`
	BoundAtUnixMs int64  `json:"bound_at_unix_ms,omitempty"`
	NeedsReauth   bool   `json:"needs_reauth,omitempty"`
	ReauthUnknown bool   `json:"reauth_unknown,omitempty"`
}

type DiscordPanelSpec struct {
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	Color  *int   `json:"color,omitempty"`
	Button string `json:"button,omitempty"`
}

type DiscordDeskRepostRequest struct {
	UserID    string           `json:"user_id"`
	GuildID   string           `json:"guild_id"`
	ChannelID string           `json:"channel_id,omitempty"`
	Panel     DiscordPanelSpec `json:"panel"`
}

type DiscordDeskRepostReply struct {
	MessageID string   `json:"message_id,omitempty"`
	Error     string   `json:"error,omitempty"`
	Code      rpc.Code `json:"code"`
}
