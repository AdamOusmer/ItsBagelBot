// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

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
	// DroppedPins names every pinned SLOT whose role id no longer exists in
	// the guild. The fill fell back to the template for those; the
	// dashboard must say so, because a pin that silently stopped applying
	// is indistinguishable from one that never saved.
	DroppedPins []string `json:"dropped_pins,omitempty"`
	// Code is the machine-readable failure, "" on success. The console
	// switches on this instead of matching Error's English prose, which is
	// translated and rewordable. See CodeInvalid.
	Code string `json:"code,omitempty"`
	// Fields names the rejected config fields by their JSON tag when Code
	// is CodeInvalid, so the dashboard can highlight the inputs rather than
	// showing one banner over a form with thirty of them.
	Fields []string `json:"fields,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// CodeInvalid rejects a request whose config-bearing fields do not pass
// ddiscord.ValidateConfig. It fires BEFORE the guild binds, so a refused
// setup has touched neither Valkey nor Discord.
const CodeInvalid = "invalid"

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
