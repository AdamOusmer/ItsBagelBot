// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordoutgress

import (
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// The ticket desk's orchestrations. They live here, rather than as a
// sequence of Commands the engine emits, for the reason the package doc gives:
// each of them needs a value only Discord's own response carries, and the next
// step depends on it.
//
//   - open: the channel's id does not exist until the create returns, and the
//     card posted into it has an id the claim edit needs afterwards.
//   - claim: editing that card in place needs the message id to still be a
//     message (a 404 here is a real outcome, not a dropped Command).
//   - close: pages a whole channel's history, uploads a file built from it,
//     and only then decides whether to move or delete the channel. Split into
//     Commands, a failure halfway leaves a ticket that is closed in the
//     database and open in Discord.
//
// All three ride the DEFAULT lane's rate budget, not the moderation lane: a
// ticket is a support conversation, not a moderation action, and letting desk
// traffic share the lane a timeout or a ban drains would put a raid's
// mod-actions behind someone's transcript upload.

// TicketOpenRequest is bagel.rpc.discord-outgress.ticket.open: create the
// private channel and post the opening card into it, in one round trip. The
// engine has already decided the name, the category and the overwrites, and
// built the embed -- outgress performs the two REST calls and returns both ids.
type TicketOpenRequest struct {
	GuildID    string                           `json:"guild_id"`
	Name       string                           `json:"name"`
	ParentID   string                           `json:"parent_id,omitempty"`
	Overwrites []discordapi.PermissionOverwrite `json:"overwrites,omitempty"`
	// Content is the message text alongside the card, normally the opener's
	// mention so they are pinged into their own ticket.
	Content string                `json:"content,omitempty"`
	Embed   ddiscord.Embed        `json:"embed"`
	Buttons []ddiscord.ButtonSpec `json:"buttons,omitempty"`
}

// TicketOpenReply carries the created channel and the card posted into it. A
// non-empty ChannelID with an Error set means the channel exists but the card
// does not: the caller must still record (or roll back) the channel.
type TicketOpenReply struct {
	ChannelID string `json:"channel_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

// TicketClaimRequest is bagel.rpc.discord-outgress.ticket.claim: edit the
// opening card so its footer names the staff member handling the ticket.
// Content is resent unchanged because Discord's message PATCH replaces the
// content field rather than leaving an omitted one alone.
type TicketClaimRequest struct {
	ChannelID string         `json:"channel_id"`
	MessageID string         `json:"message_id"`
	Content   string         `json:"content,omitempty"`
	Embed     ddiscord.Embed `json:"embed"`
	// Note is posted into the channel after the edit, so the people watching
	// see the claim without re-reading the pinned card. Empty posts nothing.
	Note string `json:"note,omitempty"`
}

type TicketClaimReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// TicketCloseSummary is what the close card says, minus the message count,
// which outgress only learns by paging the channel.
type TicketCloseSummary struct {
	Opener         string `json:"opener,omitempty"`
	Closer         string `json:"closer,omitempty"`
	OpenedAtUnixMs int64  `json:"opened_at_unix_ms,omitempty"`
}

// TicketCloseRequest is bagel.rpc.discord-outgress.ticket.close: the whole
// close sequence. Transcript off skips the paging and the upload but still
// posts the summary; an empty ArchiveCategoryID deletes the channel instead of
// moving it.
type TicketCloseRequest struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	// TicketID is the discord-data row id. It is what makes the close
	// summary idempotent: outgress remembers the summary it posted against
	// this id, so a retried close (the engine's deadline expired, the user
	// pressed the button twice) does not stack a second card and a second
	// transcript in the log channel. Zero -- the pure-Valkey fallback, which
	// has no row ids -- skips the memo and posts every time.
	TicketID          int                `json:"ticket_id,omitempty"`
	ChannelName       string             `json:"channel_name,omitempty"`
	OpenerID          string             `json:"opener_id,omitempty"`
	Transcript        bool               `json:"transcript,omitempty"`
	LogChannelID      string             `json:"log_channel_id,omitempty"`
	ArchiveCategoryID string             `json:"archive_category_id,omitempty"`
	StaffRoleIDs      []string           `json:"staff_role_ids,omitempty"`
	Summary           TicketCloseSummary `json:"summary"`
}

// TicketCloseReply reports what happened. TranscriptBody travels BACK to the
// engine rather than being written by outgress: the ticket row and every other
// discord-data write in this lifecycle already happen on the engine side, and
// splitting the row's transcript column off to a second writer would mean two
// processes racing to describe one close. The cost is the body crossing NATS
// twice, bounded at 2 MiB by ddiscord.TranscriptByteCap -- 4 MiB of the 8 MiB
// max_payload in the worst case, which only a pasted-logs ticket ever reaches.
type TicketCloseReply struct {
	MessageCount   int    `json:"message_count,omitempty"`
	TranscriptBody string `json:"transcript_body,omitempty"`
	// Truncated marks a transcript that is missing its oldest messages,
	// because the message cap tripped or a page of history failed. It travels
	// to the ticket row so a reader of the stored transcript knows the same
	// thing the person reading the attached file does.
	Truncated         bool   `json:"truncated,omitempty"`
	ArchivedChannelID string `json:"archived_channel_id,omitempty"`
	Error             string `json:"error,omitempty"`
	Code              string `json:"code,omitempty"`
}

// TicketMemberAddRequest is bagel.rpc.discord-outgress.ticket.add: grant one
// member access to an existing ticket channel (/ticket add). It writes ONE
// permission overwrite rather than going through channel.modify, because
// Discord's channel PATCH replaces the whole overwrite array -- resending it
// would mean the engine reading every existing overwrite back first, and
// getting that wrong opens a private ticket to the server.
type TicketMemberAddRequest struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
}

type TicketMemberAddReply struct {
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

// TicketPanelRequest is bagel.rpc.discord-outgress.ticket.panel: post the
// persistent desk panel and hand back the message's id.
//
// It is an RPC and not the fire-and-forget PostPanel Command the desk used to
// emit for exactly one reason: the id. Without it the engine records a desk
// pointer with an empty message id, and "repost the panel" can only stack a
// second panel under the first because it has nothing to delete.
type TicketPanelRequest struct {
	GuildID   string                `json:"guild_id"`
	ChannelID string                `json:"channel_id"`
	Content   string                `json:"content,omitempty"`
	Embed     ddiscord.Embed        `json:"embed"`
	Buttons   []ddiscord.ButtonSpec `json:"buttons,omitempty"`
}

// TicketPanelReply carries the posted panel's id, or the reason there is none.
type TicketPanelReply struct {
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}
