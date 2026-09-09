// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package outgress

import (
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
)

// Reply codes. Every dashboard-facing Discord reply carries one next to its
// human-readable Error, because the console used to branch on substrings of
// that message ("already linked to another Twitch channel") -- which made
// the message text a wire contract nobody could edit, and made a translated
// or reworded error silently change behaviour. The message stays for one
// release so an older console keeps working; new branching switches on the
// code.
//
// The generic half of this set is the shared vocabulary in
// internal/domain/rpc, aliased rather than re-declared: these were plain
// string constants spelling "invalid", "conflict" and "forbidden" a second
// time, and two spellings of one wire value is how a service drifts from the
// code the console switches on. The Discord-specific half stays declared
// here, typed rpc.Code, because the distinctions it names (which Discord
// failure this was) are not ones every service in the fleet has.
//
// Every Code field is serialized WITHOUT omitempty, deliberately. CodeOK is
// the empty string, so omitempty dropped the field entirely on the success
// path and the console could not tell "this service sends codes and nothing
// went wrong" from "this reply came from a build that predates codes" -- the
// exact ambiguity codes were added to remove. The four bytes are worth it.
const (
	// CodeOK is the empty code an untroubled reply carries.
	CodeOK = rpc.CodeOK
	// CodeForbidden: Discord refused the call (missing permissions).
	CodeForbidden = rpc.CodeForbidden
	// CodeInvalid: the request itself was malformed.
	CodeInvalid = rpc.CodeInvalid
	// CodeNotFound: Discord answered 404 for the channel the call named --
	// deleted, or never in this guild. Distinct from CodeInvalid because
	// nothing about the request is wrong; the world changed underneath it.
	CodeNotFound = rpc.CodeNotFound
	// CodeConflict: the stored settings moved on since the caller read
	// them, so the write was refused rather than applied over someone
	// else's. The dashboard reloads instead of retrying the same body.
	// Merge note (2026-09-05): the per-guild config RPC arrived with a
	// parallel DiscordCode* set that duplicated every code here except this
	// one. Two names for one wire value is how the console ends up
	// switching on a constant the service stopped sending, so the sets were
	// folded into this one and the missing code added.
	CodeConflict = rpc.CodeConflict

	// CodeBoundElsewhere: the guild belongs to a different broadcaster.
	CodeBoundElsewhere rpc.Code = "bound_elsewhere"
	// CodeNotBound: no binding exists yet for this guild.
	CodeNotBound rpc.Code = "not_bound"
	// CodeDiscordUnavailable: outgress has no usable Discord client, or
	// Discord itself failed in a way a retry might fix. Not the shared
	// CodeUnavailable: the console renders this one as "Discord is having
	// trouble", which is a different sentence from a generic dependency
	// being slow, and it is the only one of the two it can act on.
	CodeDiscordUnavailable rpc.Code = "discord_unavailable"
	// CodeRateLimited: Discord's 429. The dashboard asks the user to wait.
	CodeRateLimited rpc.Code = "rate_limited"
	// CodeTimeout: the handler ran out of time before Discord answered.
	// The console retries this one; it does not retry a refusal.
	//
	// Kept as its own value rather than folded into the shared
	// CodeUnavailable: the console's DISCORD_CODES set (see
	// console/dashboard/src/lib/server/discord-store.ts) lists "timeout"
	// and maps it to its own message key, and a code outside that set reads
	// as the empty code, which the page renders as success. Renaming the
	// wire value would therefore turn a timed-out setup into a silent
	// "worked". Rejected for the sake of one fewer constant.
	CodeTimeout rpc.Code = "timeout"
	// CodeUnknown: the call failed and nothing above classified it. This
	// exists so a non-empty Error can never travel with an empty Code: the
	// console reads "" as success, so an unclassified failure carrying ""
	// was silently rendered as one. Spelled "unknown" rather than the
	// shared CodeInternal for the same reason CodeTimeout is not
	// CodeUnavailable -- the console only knows this spelling.
	CodeUnknown rpc.Code = "unknown"
)

// DiscordSetupRequest is bagel.rpc.dingress.discord.setup. UserID is the
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
	// InstalledBy is the account that ran the install, recorded on the
	// binding so support can answer "who added this bot"; empty is accepted.
	//
	// Integration note (2026-09-05): this was specified as the Discord user
	// snowflake, but the dashboard never learns one -- the picker leg's user
	// token is spent inside listUserGuilds and discarded, and the install leg
	// returns a guild, not a user. Rather than add a /users/@me call whose
	// result would have to survive a redirect, the console sends the acting
	// console user's Twitch id, which is the identity its audit log already
	// records for the same action. Fits the column's MaxLen(20).
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
	// Fields names the rejected config fields by their JSON tag when Code
	// is CodeInvalid, so the dashboard can highlight the inputs rather than
	// showing one banner over a form with thirty of them.
	Fields []string `json:"fields,omitempty"`
	Error  string   `json:"error,omitempty"`
	// Code is the machine-readable failure, CodeOK on success. Merge note
	// (2026-09-05): feat/discord-roles declared its own CodeInvalid here
	// with `code,omitempty`; the shared const block above already carries
	// CodeInvalid, and the no-omitempty rule documented there wins -- a
	// dropped "code" on success is the ambiguity codes exist to remove.
	Code rpc.Code `json:"code"`
}

// DiscordLayoutRequest is bagel.rpc.dingress.discord.layout: the guild's
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
	NeedsReauth bool     `json:"needs_reauth,omitempty"`
	Error       string   `json:"error,omitempty"`
	Code        rpc.Code `json:"code"`
}

// DiscordUnbindRequest is bagel.rpc.dingress.discord.unbind: drop the
// guild→broadcaster reverse index on disconnect. Only the bound broadcaster
// can unbind.
type DiscordUnbindRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
}

type DiscordUnbindReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code"`
}

// DiscordPostRequest is bagel.rpc.dingress.discord.post: Bagel's own
// changelog/status channel, or any connected guild channel the operator names.
type DiscordPostRequest struct {
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
}

type DiscordPostReply struct {
	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code"`
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
	LastCloseCode int `json:"last_close_code,omitempty"`
	// The connect budget, straight off the bot status key. An offline bot
	// and an offline bot whose ingress has deliberately stopped dialling are
	// different answers, and on 2026-09-05 the difference was a token reset
	// nobody saw coming. Flapping means slowed down and still trying;
	// AtCeiling means stopped until ParkUntilUnixMS, which is the one number
	// that tells a streamer when to look again rather than to file a ticket.
	Flapping         bool  `json:"flapping,omitempty"`
	ConnectsInWindow int   `json:"connects_in_window,omitempty"`
	AtCeiling        bool  `json:"at_ceiling,omitempty"`
	ParkUntilUnixMS  int64 `json:"park_until_unix_ms,omitempty"`

	Error string   `json:"error,omitempty"`
	Code  rpc.Code `json:"code"`
}

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
	Code    rpc.Code        `json:"code"`
	Error   string          `json:"error,omitempty"`
}

// DiscordConfigSetRequest is bagel.rpc.dingress.discord.config.set.
// ExpectedVersion is the version the page loaded with; a mismatch is refused
// with CodeConflict rather than silently discarding whatever the other
// open tab saved.
type DiscordConfigSetRequest struct {
	UserID          string          `json:"user_id"`
	GuildID         string          `json:"guild_id"`
	Config          ddiscord.Config `json:"config"`
	ExpectedVersion int             `json:"expected_version"`
}

// DiscordConfigSetReply carries the version the settings now hold. Fields
// names the controls that failed validation when Code is CodeInvalid.
type DiscordConfigSetReply struct {
	Version int      `json:"version"`
	Fields  []string `json:"fields,omitempty"`
	Code    rpc.Code `json:"code"`
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
	// Truncated is true when the broadcaster has more bindings than the
	// listing describes (setup.MaxListedGuilds). Without it a streamer with
	// twenty-six servers sees twenty-five and no sign the list is short,
	// which reads as "Bagel lost my server" rather than "this page shows the
	// first twenty-five".
	Truncated bool     `json:"truncated,omitempty"`
	Code      rpc.Code `json:"code"`
	Error     string   `json:"error,omitempty"`
}

// DiscordGuildEntry is one connected server as the picker shows it.
type DiscordGuildEntry struct {
	GuildID string `json:"guild_id"`
	Name    string `json:"name,omitempty"`
	// IconURL is empty: the dashboard draws a monogram because the page's CSP
	// forbids Discord's image CDN, so fetching the icon hash here would cost a
	// REST field nothing renders.
	IconURL string `json:"icon_url,omitempty"`
	// MemberCount is Discord's own approximation, filled from the same
	// with_counts lookup that answers BotPresent. Zero when the bot is not in
	// the guild any more, since there is then nothing to count.
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
	// NeedsReauth is this guild's stale-grant flag, the same one status and
	// layout carry. Integration fix (2026-09-05): the server-list page has
	// always rendered three pill states (online / offline / reauth) off this
	// field, but the entry never carried it, so a guild whose grant died
	// showed as a plain "offline" and sent the streamer to wait for a
	// reconnect that cannot happen without a new authorization.
	NeedsReauth bool `json:"needs_reauth,omitempty"`
	// ReauthUnknown says the flag above was never read, so false means "we
	// do not know", not "the grant is fine".
	//
	// Integration fix (2026-09-05): the listing returns a partial list when
	// the handler's deadline passes mid-way, and the reauth lookups happen
	// after that -- every one of them then failed against the dead context
	// and reported false. A guild whose grant had died was rendered as a
	// healthy one on exactly the slow load where the streamer is already
	// suspicious. The console shows a neutral pill on this rather than green.
	ReauthUnknown bool `json:"reauth_unknown,omitempty"`
}

// DiscordPanelSpec is the ticket-desk embed's copy, as the dashboard just saved
// it. It travels on the repost request rather than being read by outgress: see
// setup.DeskRepostRequest for why outgress holds no per-guild config.
type DiscordPanelSpec struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	// Color is the embed's left bar as a decimal Discord color. Absent (null,
	// or the key left out) means "not set" and takes the brand default; 0 is
	// #000000 and is honoured.
	//
	// Integration fix (2026-09-05): this was a plain int with "zero means
	// unset", which made black the one colour the picker could not save --
	// every repost of a black panel came back brand purple. A pointer is what
	// carries the difference across JSON. The console must therefore OMIT the
	// key (or send null) for "unset" and send 0 only when the streamer really
	// picked #000000.
	Color  *int   `json:"color,omitempty"`
	Button string `json:"button,omitempty"`
}

// DiscordDeskRepostRequest is bagel.rpc.dingress.discord.desk.repost: delete
// the panel message this guild last posted and put a fresh one in its place.
// The dashboard calls it after saving the panel embed.
type DiscordDeskRepostRequest struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id"`
	// ChannelID is optional: empty reposts into the channel the previous panel
	// was in.
	ChannelID string           `json:"channel_id,omitempty"`
	Panel     DiscordPanelSpec `json:"panel"`
}

// DiscordDeskRepostReply carries the new panel message's id.
type DiscordDeskRepostReply struct {
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
	// Code carries no omitempty, matching every other reply in this file:
	// see the const block's note. Integration fix (2026-09-05): this one
	// shipped with omitempty, so a successful repost sent no "code" key at
	// all and the console's replyCode fell through to matching the English
	// error text -- the exact behaviour codes replaced.
	Code rpc.Code `json:"code"`
}
