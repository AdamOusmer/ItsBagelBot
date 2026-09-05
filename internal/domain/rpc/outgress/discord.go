// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import ddiscord "ItsBagelBot/internal/domain/discord"

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
	// InstalledBy is the Discord user snowflake that ran the install, known to
	// the dashboard from the OAuth exchange. Recorded on the binding so
	// support can answer "who added this bot"; empty is accepted.
	InstalledBy string `json:"installed_by,omitempty"`
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
	SubsChannelID    string `json:"subs_channel_id,omitempty"`
	SubsCategoryID   string `json:"subs_category_id,omitempty"`
	VIPChannelID     string `json:"vip_channel_id,omitempty"`
	VIPCategoryID    string `json:"vip_category_id,omitempty"`
	OwnerRoleID      string `json:"owner_role_id,omitempty"`
	LeadModRoleID    string `json:"lead_mod_role_id,omitempty"`
	ModsRoleID       string `json:"mods_role_id,omitempty"`
	VIPRoleID        string `json:"vip_role_id,omitempty"`
	SubscriberRoleID string `json:"subscriber_role_id,omitempty"`
	RegularsRoleID   string `json:"regulars_role_id,omitempty"`
	MemberRoleID     string `json:"member_role_id,omitempty"`
	Refused          string `json:"refused,omitempty"`
	Error            string `json:"error,omitempty"`
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

type DiscordLayoutReply struct {
	Channels []DiscordLayoutEntry `json:"channels,omitempty"`
	Roles    []DiscordLayoutEntry `json:"roles,omitempty"`
	// NeedsReauth is true when this guild's bot role predates
	// CHANGE_NICKNAME, so the premium per-guild rename is refused while the
	// avatar still applies. Discord freezes a bot's permissions at install,
	// so the only fix is the streamer re-authorizing; the dashboard shows
	// the prompt, and it clears itself the first time a rename succeeds.
	NeedsReauth bool   `json:"needs_reauth,omitempty"`
	Error       string `json:"error,omitempty"`
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
}

// DiscordPostRequest is bagel.rpc.outgress.discord.post: Bagel's own
// changelog/status channel, or any connected guild channel the operator names.
type DiscordPostRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

type DiscordPostReply struct {
	Error string `json:"error,omitempty"`
}

// Reply codes for the dashboard-facing Discord RPC. The console switches on
// these instead of matching substrings of an error message that changes
// whenever a log line is reworded. The empty string is success.
const (
	DiscordCodeOK = ""
	// DiscordCodeBoundElsewhere: the guild belongs to a different Twitch
	// channel. A broadcaster adding a second, third or tenth server is
	// ordinary and never raises this.
	DiscordCodeBoundElsewhere = "bound_elsewhere"
	// DiscordCodeNotBound: the caller does not own that guild, or nothing
	// does. The two are one code on purpose, so a caller cannot probe which
	// guild ids exist.
	DiscordCodeNotBound = "not_bound"
	// DiscordCodeConflict: the settings moved on since the caller read them.
	// The dashboard reloads rather than retrying the same write.
	DiscordCodeConflict = "conflict"
	// DiscordCodeInvalid: the settings failed validation. Fields names the
	// controls to flag.
	DiscordCodeInvalid = "invalid"
	// DiscordCodeUnavailable: the store or Discord itself could not be
	// reached. Transient; the dashboard offers a retry.
	DiscordCodeUnavailable = "discord_unavailable"
	// DiscordCodeTimeout: the reply ran out of time part-way and carries
	// whatever was gathered. Unlike DiscordCodeUnavailable the payload is
	// usable, so the dashboard renders it and says the list may be short
	// rather than replacing the page with an error.
	DiscordCodeTimeout = "timeout"
)

// DiscordConfigGetRequest is bagel.rpc.dingress.discord.config.get: one
// guild's settings, for the dashboard page. UserID is the Twitch broadcaster
// and is checked against the guild's binding.
type DiscordConfigGetRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

// DiscordConfigGetReply carries the settings and the version to echo back on
// save. Found is false for a guild that was bound but never saved; Config is
// then the zero value, which is what the page renders anyway.
type DiscordConfigGetReply struct {
	Config  ddiscord.Config `json:"config"`
	Version int             `json:"version"`
	Found   bool            `json:"found"`
	Code    string          `json:"code"`
	Error   string          `json:"error,omitempty"`
}

// DiscordConfigSetRequest is bagel.rpc.dingress.discord.config.set.
// ExpectedVersion is the version the page loaded with; a mismatch is refused
// with DiscordCodeConflict rather than silently discarding whatever the other
// open tab saved.
type DiscordConfigSetRequest struct {
	UserID          string          `json:"user_id"`
	GuildID         string          `json:"guild_id"`
	Config          ddiscord.Config `json:"config"`
	ExpectedVersion int             `json:"expected_version"`
}

// DiscordConfigSetReply carries the version the settings now hold. Fields
// names the controls that failed validation when Code is DiscordCodeInvalid.
type DiscordConfigSetReply struct {
	Version int      `json:"version"`
	Fields  []string `json:"fields,omitempty"`
	Code    string   `json:"code"`
	Error   string   `json:"error,omitempty"`
}

// DiscordGuildsListRequest is bagel.rpc.dingress.discord.guilds.list: every
// server this broadcaster connected, for the server picker.
type DiscordGuildsListRequest struct {
	UserID string `json:"user_id"`
}

// DiscordGuildsListReply carries the servers, oldest binding first.
type DiscordGuildsListReply struct {
	Guilds []DiscordGuildEntry `json:"guilds"`
	Code   string              `json:"code"`
	Error  string              `json:"error,omitempty"`
}

// DiscordGuildEntry is one connected server as the picker shows it.
type DiscordGuildEntry struct {
	GuildID string `json:"guild_id"`
	Name    string `json:"name,omitempty"`
	// IconURL is empty: the dashboard draws a monogram because the page's CSP
	// forbids Discord's image CDN, so fetching the icon hash here would cost a
	// REST field nothing renders.
	IconURL string `json:"icon_url,omitempty"`
	// MemberCount is zero until the REST client grows a with_counts variant of
	// GetGuild (it lands with the bot-status work). The picker shows the name
	// and the presence pill, neither of which needs it.
	MemberCount int `json:"member_count,omitempty"`
	// BotPresent is false when Discord answers 403 or 404 for the guild: the
	// bot was kicked, or the server was deleted. The entry is still listed,
	// because the binding is still there and the streamer needs to see it to
	// disconnect or re-invite.
	BotPresent bool `json:"bot_present"`
	// BoundAtUnixMs is when the streamer connected this server. The listing is
	// already oldest-binding-first, so the picker does not need it to sort;
	// it is here for the card's "connected since".
	BoundAtUnixMs int64 `json:"bound_at_unix_ms,omitempty"`
}
