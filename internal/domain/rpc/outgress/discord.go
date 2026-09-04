// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

// Reply codes. Every dashboard-facing Discord reply carries one next to its
// human-readable Error, because the console used to branch on substrings of
// that message ("already linked to another Twitch channel") -- which made
// the message text a wire contract nobody could edit, and made a translated
// or reworded error silently change behaviour. The message stays for one
// release so an older console keeps working; new branching switches on the
// code.
const (
	// CodeOK is the empty code an untroubled reply carries.
	CodeOK = ""
	// CodeBoundElsewhere: the guild belongs to a different broadcaster.
	CodeBoundElsewhere = "bound_elsewhere"
	// CodeNotBound: no binding exists yet for this guild.
	CodeNotBound = "not_bound"
	// CodeDiscordUnavailable: outgress has no usable Discord client, or
	// Discord itself failed in a way a retry might fix.
	CodeDiscordUnavailable = "discord_unavailable"
	// CodeForbidden: Discord refused the call (missing permissions).
	CodeForbidden = "forbidden"
	// CodeRateLimited: Discord's 429. The dashboard asks the user to wait.
	CodeRateLimited = "rate_limited"
	// CodeInvalid: the request itself was malformed.
	CodeInvalid = "invalid"
)

// DiscordSetupRequest is bagel.rpc.outgress.discord.setup. UserID is the
// Twitch broadcaster the guild binds to. The dashboard proves the caller
// installed the bot in GuildID (OAuth code exchange) before asking; outgress
// only refuses a guild already bound to a different broadcaster.
type DiscordSetupRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
	// Subscribers mirrors the streamer's subscriber toggle so the fill can
	// skip the Subscriber role and its locked category when the tier is off.
	Subscribers bool `json:"subscribers,omitempty"`
	// PinnedRoles is slot -> existing guild role id (ddiscord.RoleSlots).
	// The fill adopts these instead of creating or name-matching, so a
	// server whose staff role is already called something else keeps it.
	PinnedRoles map[string]string `json:"pinned_roles,omitempty"`
}

// DiscordSetupReply is the filled template the dashboard writes into the
// Discord module blob. Refused is set when the guild already looked lived-in;
// the ids are then whatever existing channels matched the template by name.
type DiscordSetupReply struct {
	GuildID          string `json:"guild_id,omitempty"`
	LiveChannelID    string `json:"live_channel_id,omitempty"`
	ClipsChannelID   string `json:"clips_channel_id,omitempty"`
	WelcomeChannelID string `json:"welcome_channel_id,omitempty"`
	VoiceHubID       string `json:"voice_hub_id,omitempty"`
	LogChannelID     string `json:"log_channel_id,omitempty"`
	TicketChannelID  string `json:"ticket_channel_id,omitempty"`
	TicketCategoryID string `json:"ticket_category_id,omitempty"`
	// TicketArchiveCategoryID is the Archive category closed tickets move
	// into; empty when the guild predates it.
	TicketArchiveCategoryID string `json:"ticket_archive_category_id,omitempty"`
	SubsChannelID           string `json:"subs_channel_id,omitempty"`
	SubsCategoryID          string `json:"subs_category_id,omitempty"`
	VIPChannelID            string `json:"vip_channel_id,omitempty"`
	VIPCategoryID           string `json:"vip_category_id,omitempty"`
	OwnerRoleID             string `json:"owner_role_id,omitempty"`
	LeadModRoleID           string `json:"lead_mod_role_id,omitempty"`
	ModsRoleID              string `json:"mods_role_id,omitempty"`
	VIPRoleID               string `json:"vip_role_id,omitempty"`
	SubscriberRoleID        string `json:"subscriber_role_id,omitempty"`
	RegularsRoleID          string `json:"regulars_role_id,omitempty"`
	MemberRoleID            string `json:"member_role_id,omitempty"`
	Refused                 string `json:"refused,omitempty"`
	Error                   string `json:"error,omitempty"`
	// Code is the machine-readable error class; "" means ok. Never omitted
	// so the console can switch on it without a presence check.
	Code string `json:"code"`
}

// DiscordLayoutRequest is bagel.rpc.outgress.discord.layout: the guild's
// channels and roles so the dashboard can offer pickers on a lived-in server.
type DiscordLayoutRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

// DiscordLayoutEntry is one channel or role. Type is Discord's channel type
// (0 text, 2 voice, 4 category); roles carry 0.
type DiscordLayoutEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type,omitempty"`
}

// DiscordGuildInfo is the server card: what the guild is called, what it
// looks like, and how many people are in it.
type DiscordGuildInfo struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// IconURL is already a CDN URL, not Discord's icon hash: the console
	// must never have to know how to assemble one.
	IconURL string `json:"icon_url,omitempty"`
	// MemberCount is Discord's own approximation, not a live count.
	MemberCount int `json:"member_count,omitempty"`
}

type DiscordLayoutReply struct {
	Channels []DiscordLayoutEntry `json:"channels,omitempty"`
	Roles    []DiscordLayoutEntry `json:"roles,omitempty"`
	// Categories are split out of Channels rather than left mixed in with a
	// type discriminator: every picker in the dashboard wants one list or
	// the other, never both, and doing the split here means the console
	// does not carry a copy of Discord's channel-type numbering.
	Categories []DiscordLayoutEntry `json:"categories,omitempty"`
	// Guild is nil when the guild lookup itself failed; the rest of the
	// layout is still worth returning, since the pickers do not need it.
	Guild *DiscordGuildInfo `json:"guild,omitempty"`
	// BotOnline and BotSinceUnixMS come from the bot status key ingress
	// publishes (internal/domain/discord.BotStatusKey), not from this
	// request: outgress holds no gateway connection of its own.
	BotOnline      bool  `json:"bot_online,omitempty"`
	BotSinceUnixMS int64 `json:"bot_since_unix_ms,omitempty"`
	LastCloseCode  int   `json:"last_close_code,omitempty"`
	// NeedsReauth is true when this guild's bot role predates
	// CHANGE_NICKNAME, so the premium per-guild rename is refused while the
	// avatar still applies. Discord freezes a bot's permissions at install,
	// so the only fix is the streamer re-authorizing; the dashboard shows
	// the prompt, and it clears itself the first time a rename succeeds.
	NeedsReauth bool   `json:"needs_reauth,omitempty"`
	Error       string `json:"error,omitempty"`
	Code        string `json:"code,omitempty"`
}

// DiscordUnbindRequest is bagel.rpc.outgress.discord.unbind: drop the
// guild→broadcaster reverse index on disconnect. Only the bound broadcaster
// can unbind.
type DiscordUnbindRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

type DiscordUnbindReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// DiscordPostRequest is bagel.rpc.outgress.discord.post: Bagel's own
// changelog/status channel, or any connected guild channel the operator names.
type DiscordPostRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

type DiscordPostReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// DiscordStatusRequest is bagel.rpc.dingress.discord.status: is the bot
// online, and is it in this guild. Answered from the bot status key plus one
// GetGuildWithCounts call, so it is cheap enough for the dashboard to poll
// while a page is open -- which is why it is a separate subject from layout
// rather than more fields on it.
type DiscordStatusRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

// DiscordStatusReply answers for two independent things, and they fail
// independently: Online describes the fleet's one gateway session, while
// GuildPresent describes this guild. A bot that is online but was kicked
// from the server reports Online true and GuildPresent false, which is the
// exact case the old dashboard could not tell apart from "bot down".
type DiscordStatusReply struct {
	Online         bool   `json:"online,omitempty"`
	SinceUnixMS    int64  `json:"since_unix_ms,omitempty"`
	SessionResumes int    `json:"session_resumes,omitempty"`
	GuildPresent   bool   `json:"guild_present,omitempty"`
	GuildName      string `json:"guild_name,omitempty"`
	IconURL        string `json:"icon_url,omitempty"`
	MemberCount    int    `json:"member_count,omitempty"`
	NeedsReauth    bool   `json:"needs_reauth,omitempty"`
	// LastCloseCode explains an offline bot (see
	// internal/domain/discord.CloseCodeMessage). It survives reconnects, so
	// a non-zero value on an online bot is history, not a fault.
	LastCloseCode int    `json:"last_close_code,omitempty"`
	Error         string `json:"error,omitempty"`
	Code          string `json:"code,omitempty"`
}
