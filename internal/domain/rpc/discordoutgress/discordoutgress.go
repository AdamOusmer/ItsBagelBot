// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discordoutgress is the wire contract for app/discord/engine's
// calls into app/discord/outgress that internal/domain/discord's Command
// cannot carry.
//
// Command is deliberately "one Discord REST call, described rather than
// performed" with no reply channel (see that type's doc). Everything engine
// needs to decide maps onto that shape except a handful of operations whose
// caller needs a value ONLY Discord's response carries -- a created
// channel's id, whether a purge actually found messages to delete -- or
// whose correctness depends on state that must live wherever the REST call
// that mutated it runs. Ticket channels, join-to-create voice clones, and
// the go-live message outgress edits in place on stream.offline are all in
// this bucket: "create a channel, then use the id it comes back with" is not
// expressible as a fire-and-forget Command no matter how the Type enum
// grows, because the id does not exist until the call returns.
//
// This package is NOT internal/domain/discord: it does not touch the
// committed Event/Command contract at all, and it is not the dashboard's
// bagel.rpc.dingress / bagel.rpc.outgress surface either (see
// internal/domain/rpc/outgress) -- conflating an internal engine->outgress
// call with a console-facing one would let a console client reach an
// operation it was never meant to invoke, and would tie this internal
// contract's evolution to dashboard compatibility it does not need. This
// prefix (bagel.rpc.discord-outgress, see app/discord/engine and
// app/discord/outgress's config) is private to the two of them.
package discordoutgress

import (
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// ChannelCreateRequest is bagel.rpc.discord-outgress.channel.create: engine
// has already decided a channel is needed (a ticket, a join-to-create
// clone); outgress performs the REST call and returns the id engine cannot
// otherwise learn.
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

// ChannelDeleteRequest is bagel.rpc.discord-outgress.channel.delete: closing
// a ticket, or reaping an empty voice clone.
type ChannelDeleteRequest struct {
	ChannelID string `json:"channel_id"`
}

type ChannelDeleteReply struct {
	Error string `json:"error,omitempty"`
}

// ChannelModifyRequest is bagel.rpc.discord-outgress.channel.modify: the
// voice-clone owner's /voice name|limit|lock|unlock commands. Zero-valued
// Name/UserLimit/Overwrites each mean "leave this field as is" -- the same
// convention discordapi.ModifyChannel already uses, carried through
// unchanged rather than inventing a sentinel this RPC would have to
// translate back out of.
type ChannelModifyRequest struct {
	ChannelID  string                           `json:"channel_id"`
	Name       string                           `json:"name,omitempty"`
	UserLimit  int                              `json:"user_limit,omitempty"`
	Overwrites []discordapi.PermissionOverwrite `json:"overwrites,omitempty"`
}

type ChannelModifyReply struct {
	Error string `json:"error,omitempty"`
}

// MemberMoveRequest is bagel.rpc.discord-outgress.member.move: dropping a
// join-to-create voice clone's owner into their new channel.
type MemberMoveRequest struct {
	GuildID   string `json:"guild_id"`
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
}

type MemberMoveReply struct {
	Error string `json:"error,omitempty"`
}

// PurgeRequest is bagel.rpc.discord-outgress.channel.purge: /purge. Discord's
// bulk-delete needs the target ids, which only a preceding list call can
// supply, so this is a list-then-bulk-delete round trip on outgress's side,
// not a single Command.
type PurgeRequest struct {
	ChannelID string `json:"channel_id"`
	Count     int    `json:"count"`
}

type PurgeReply struct {
	Deleted int    `json:"deleted,omitempty"`
	Error   string `json:"error,omitempty"`
}

// LiveOnlineRequest is bagel.rpc.discord-outgress.live.online. Engine has
// already decided the module is on and the category is allowed, and built
// the embed; the @Live role grant/revoke is NOT part of this call -- it
// rides an ordinary TypeAddRole/TypeRemoveRole Command instead, since
// AddMemberRole/RemoveMemberRole are single idempotent REST calls with
// nothing for engine to learn back, exactly what Command already exists to
// describe. This RPC covers only the one thing that needs a synchronous
// round trip: outgress owns the idempotency (has THIS stream's go-live
// already been posted?) because that answer is keyed on a message id only
// outgress ever learns, from the SendEmbed call only outgress is allowed to
// make. A repeat call for a stream already announced is a deliberate no-op,
// matching the pre-split discordOnline.
type LiveOnlineRequest struct {
	GuildID   string         `json:"guild_id"`
	ChannelID string         `json:"channel_id"`
	Embed     ddiscord.Embed `json:"embed"`
}

type LiveOnlineReply struct {
	Error string `json:"error,omitempty"`
}

// LiveOfflineRequest is bagel.rpc.discord-outgress.live.offline: flip the
// remembered go-live message to OfflineContent and forget it. A guild with
// no remembered message (never announced, or already edited) is a no-op.
type LiveOfflineRequest struct {
	GuildID string `json:"guild_id"`
}

type LiveOfflineReply struct {
	Error string `json:"error,omitempty"`
}
