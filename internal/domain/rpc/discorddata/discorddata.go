// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discorddata holds the shared wire types for the discord-data
// service RPC surface (bagel.rpc.discord-data.*), the MySQL-backed store
// behind Discord's guild bindings, ticket desk and member XP.
//
// The engine and outgress reach this surface through
// internal/discordstore.NewRPC rather than calling it directly, so these types
// are the contract between app/db/discord's handlers and that client, not
// something a module ever sees.
//
// Every reply carries a machine-readable Code alongside the human Error, so
// callers switch on a constant instead of matching substrings of a message
// that changes whenever a log line is reworded.
package discorddata

// Subject verbs, appended to the service prefix (default
// "bagel.rpc.discord-data"). Kept here so the server's subscriptions and the
// client's requests read from one list.
const (
	VerbBindingGet           = "binding.get"
	VerbBindingSet           = "binding.set"
	VerbBindingDelete        = "binding.delete"
	VerbBindingByBroadcaster = "binding.by_broadcaster"

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

// Reply codes. The empty string is success; every other value names one
// refusal a caller can act on differently from a generic failure.
const (
	// CodeOK is the zero value: the call did what it was asked to.
	CodeOK = ""
	// CodeBoundElsewhere: the guild is already bound to a different
	// broadcaster (or the broadcaster to a different guild). The unique
	// indexes on guild_bindings make this a database invariant, not a race.
	CodeBoundElsewhere = "bound_elsewhere"
	// CodeNotBound: no binding row exists for the guild the caller named.
	CodeNotBound = "not_bound"
	// CodeLimit: the opener already holds OpenLimit tickets in this guild.
	CodeLimit = "limit"
	// CodeNotFound: the ticket or transcript the caller addressed is absent.
	CodeNotFound = "not_found"
	// CodeInvalid: the request itself is malformed (missing id, bad limit).
	CodeInvalid = "invalid"
	// CodeInternal: the store failed. The Error field carries the detail.
	CodeInternal = "internal"
)

// Ticket status values, mirroring the ent enum on the tickets table.
const (
	StatusOpen     = "open"
	StatusClaimed  = "claimed"
	StatusClosed   = "closed"
	StatusArchived = "archived"
)

// BindingGetRequest resolves one guild to the broadcaster it is bound to.
type BindingGetRequest struct {
	GuildID string `json:"guild_id"`
}

// BindingGetReply carries the bound broadcaster. Found is false (with an empty
// Error) when the guild has no binding, which is an ordinary state, not a
// failure.
type BindingGetReply struct {
	BroadcasterID uint64 `json:"broadcaster_id"`
	Found         bool   `json:"found"`
	Error         string `json:"error,omitempty"`
	Code          string `json:"code,omitempty"`
}

// BindingSetRequest binds a guild to a broadcaster. Re-binding the same pair
// is idempotent; binding either half to a different partner is refused with
// CodeBoundElsewhere.
type BindingSetRequest struct {
	GuildID       string `json:"guild_id"`
	BroadcasterID uint64 `json:"broadcaster_id"`
	// InstalledBy is the Discord user snowflake that ran the setup.
	InstalledBy string `json:"installed_by,omitempty"`
}

// BindingSetReply reports whether the binding now holds.
type BindingSetReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// BindingDeleteRequest unbinds a guild. BroadcasterID, when non-zero, guards
// the delete: a stale unbind for a guild that has since been re-bound to
// someone else is refused rather than silently dropping the new owner's row.
type BindingDeleteRequest struct {
	GuildID       string `json:"guild_id"`
	BroadcasterID uint64 `json:"broadcaster_id,omitempty"`
}

// BindingDeleteReply reports whether the guild is now unbound.
type BindingDeleteReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// BindingByBroadcasterRequest is the reverse lookup: which guild, if any, this
// broadcaster installed the bot into.
type BindingByBroadcasterRequest struct {
	BroadcasterID uint64 `json:"broadcaster_id"`
}

// BindingByBroadcasterReply carries the bound guild.
type BindingByBroadcasterReply struct {
	GuildID string `json:"guild_id"`
	Found   bool   `json:"found"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
}

// TicketOpenRequest records a newly created ticket channel. OpenLimit is the
// guild's configured per-member cap, passed in rather than read here so the
// data service never has to reach back into the modules blob for config.
type TicketOpenRequest struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	OpenerID  string `json:"opener_id"`
	Subject   string `json:"subject,omitempty"`
	OpenLimit int    `json:"open_limit,omitempty"`
	// PanelMessageID is the ticket card the engine already posted into the
	// channel; claiming edits that message's footer in place.
	PanelMessageID string `json:"panel_message_id,omitempty"`
}

// TicketOpenReply carries the new row's id and the opener's resulting open
// count. On CodeLimit the count is the opener's current (unchanged) total, so
// the caller can name the number in its refusal.
type TicketOpenReply struct {
	TicketID  int    `json:"ticket_id"`
	OpenCount int    `json:"open_count"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

// TicketClaimRequest marks the ticket in ChannelID as claimed by StaffID.
type TicketClaimRequest struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	StaffID   string `json:"staff_id"`
}

// TicketClaimReply carries the claimed ticket's id.
type TicketClaimReply struct {
	TicketID int    `json:"ticket_id"`
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
}

// TicketCloseRequest closes the ticket in ChannelID. A non-empty
// ArchivedChannelID means the channel was moved to the archive category rather
// than deleted, and the row lands in status archived instead of closed.
type TicketCloseRequest struct {
	GuildID           string `json:"guild_id"`
	ChannelID         string `json:"channel_id"`
	ClosedBy          string `json:"closed_by"`
	ArchivedChannelID string `json:"archived_channel_id,omitempty"`
}

// TicketCloseReply carries the closed ticket's id and its opener, so the
// caller can address the close summary without a second lookup.
type TicketCloseReply struct {
	TicketID int    `json:"ticket_id"`
	OpenerID string `json:"opener_id"`
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
}

// TicketGetRequest resolves a ticket from the channel a button was pressed in.
type TicketGetRequest struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
}

// TicketGetReply carries the ticket. Found is false (with an empty Error) when
// the channel holds no ticket.
type TicketGetReply struct {
	Ticket Ticket `json:"ticket"`
	Found  bool   `json:"found"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

// TicketCountRequest asks how many live (open or claimed) tickets one member
// holds in one guild.
//
// Not in the original verb table, and deliberately added: without it the desk
// can only learn the count by attempting an open, which means creating a
// Discord channel and deleting it again every time somebody at their limit
// presses the button -- two REST calls on exactly the path that should be
// cheap. The count is also what numbers the channel (ticket-<name>-<n>), which
// must be decided BEFORE the channel is created.
type TicketCountRequest struct {
	GuildID  string `json:"guild_id"`
	OpenerID string `json:"opener_id"`
}

// TicketCountReply carries the member's live-ticket count.
type TicketCountReply struct {
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// TicketListRequest pages one guild's tickets. Status is empty for every
// status, or one of the Status* constants. Cursor is the previous reply's
// NextCursor; the listing is newest-first by ticket id.
type TicketListRequest struct {
	GuildID string `json:"guild_id"`
	Status  string `json:"status,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
}

// TicketListReply carries one page. NextCursor is empty on the last page.
type TicketListReply struct {
	Tickets    []Ticket `json:"tickets"`
	NextCursor string   `json:"next_cursor,omitempty"`
	Error      string   `json:"error,omitempty"`
	Code       string   `json:"code,omitempty"`
}

// Ticket is one support channel's stored row. Timestamps are unix
// milliseconds, zero when unset, matching the rest of the fleet's RPC wire
// format (a JSON time string would need every consumer to agree on a layout).
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

// TranscriptPutRequest stores (or replaces) one ticket's rendered transcript.
type TranscriptPutRequest struct {
	TicketID     int    `json:"ticket_id"`
	Body         string `json:"body"`
	MessageCount int    `json:"message_count"`
}

// TranscriptPutReply reports whether the transcript was stored.
type TranscriptPutReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// TranscriptGetRequest reads one ticket's transcript back.
type TranscriptGetRequest struct {
	TicketID int `json:"ticket_id"`
}

// TranscriptGetReply carries the stored transcript.
type TranscriptGetReply struct {
	Body           string `json:"body"`
	MessageCount   int    `json:"message_count"`
	StoredAtUnixMs int64  `json:"stored_at_unix_ms"`
	Found          bool   `json:"found"`
	Error          string `json:"error,omitempty"`
	Code           string `json:"code,omitempty"`
}

// XPGetRequest reads one member's standing.
type XPGetRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

// XPGetReply carries the member's stored XP. Found is false for a member who
// has never earned any; XP and Level are then zero, which is the same answer
// the caller wants to render.
type XPGetReply struct {
	XPValue         int64  `json:"xp"`
	Level           int    `json:"level"`
	LastDailyUnixMs int64  `json:"last_daily_unix_ms,omitempty"`
	Found           bool   `json:"found"`
	Error           string `json:"error,omitempty"`
	Code            string `json:"code,omitempty"`
}

// XPAddRequest credits Delta XP to one member. The cooldown that decides
// whether a message earns XP at all stays in Valkey on the engine side; by the
// time a request reaches here the award is already decided.
type XPAddRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	Delta   int64  `json:"delta"`
}

// XPAddReply carries the member's new total. LeveledUp is true only when this
// delta crossed a level boundary, so the caller announces a level-up exactly
// once even if it retries the read.
type XPAddReply struct {
	XPValue   int64  `json:"xp"`
	Level     int    `json:"level"`
	LeveledUp bool   `json:"leveled_up"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

// XPDailyRequest claims one member's daily bonus.
type XPDailyRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	Amount  int64  `json:"amount"`
}

// XPDailyReply reports the claim. Granted is false when the member is still
// inside the 24h window; NextUnixMs is then when they may claim again.
type XPDailyReply struct {
	Granted    bool   `json:"granted"`
	XPValue    int64  `json:"xp"`
	Level      int    `json:"level"`
	NextUnixMs int64  `json:"next_unix_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
}

// XPTopRequest reads one guild's leaderboard, highest first.
type XPTopRequest struct {
	GuildID string `json:"guild_id"`
	Limit   int    `json:"limit,omitempty"`
}

// XPTopReply carries the leaderboard page.
type XPTopReply struct {
	Rows  []XPRow `json:"rows"`
	Error string  `json:"error,omitempty"`
	Code  string  `json:"code,omitempty"`
}

// XPRow is one member's place on the leaderboard.
type XPRow struct {
	UserID  string `json:"user_id"`
	XPValue int64  `json:"xp"`
	Level   int    `json:"level"`
}
