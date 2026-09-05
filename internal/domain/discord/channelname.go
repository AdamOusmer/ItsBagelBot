// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strconv"
	"strings"
)

// ChannelNameMax is Discord's ceiling on a guild channel's name.
//
// 100 is the documented limit for CHANNEL_NAME; a create or a rename that
// exceeds it fails the whole REST call with a 400, which on the ticket path
// means a member presses "Open a ticket" and gets nothing. The name is built
// from a Discord username, and usernames can be 32 characters of anything, so
// this is reachable in practice rather than theoretically.
const ChannelNameMax = 100

// ticketNameHead is the fixed head of every ticket channel's name. Kept as a
// constant because both the open path and the archive path have to agree on it.
const ticketNameHead = "ticket"

// SanitizeChannelName folds an arbitrary string into the character set Discord
// normalises text-channel names into: lowercase, and only [a-z0-9-].
//
// Discord itself accepts unicode in a channel name and silently rewrites what
// it does not like, which is the problem: the name it stores is then not the
// name we asked for, and the archive rename (which prefixes the name we
// believe the channel has) drifts from reality. Normalising here means the
// name in the request is the name in the guild.
//
// A run of disallowed runes collapses to ONE hyphen rather than being dropped,
// so "Ada Lovelace" reads as "ada-lovelace" instead of "adalovelace". A string
// with nothing usable in it (a name written entirely in a non-Latin script)
// yields the empty string; callers substitute the user id, which is always
// [0-9].
func SanitizeChannelName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSep := false
	for _, r := range strings.ToLower(s) {
		if !allowedNameRune(r) {
			pendingSep = b.Len() > 0
			continue
		}
		if pendingSep {
			b.WriteByte('-')
			pendingSep = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// allowedNameRune is the [a-z0-9] test. The hyphen is NOT allowed through
// here: it is a separator SanitizeChannelName emits itself, so a name that
// already contains hyphens collapses through the same path as one containing
// spaces and can never produce a repeated "--".
func allowedNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// TicketChannelName is "ticket-<name>-<id>", where id is the ticket ROW's id.
//
// The number is the row id, not the opener's nth live ticket. The count was
// the obvious choice and is wrong: a member who opens, closes and reopens gets
// "ticket-ada-1" twice, and the archive category ends up with two
// "closed-ticket-ada-1" channels that no one can tell apart. Row ids are
// unique for the lifetime of the guild, so the name identifies the ticket in
// the sidebar, in the archive and in the transcript filename alike.
//
// A zero or negative id (the pure-Valkey fallback has no row ids) drops the
// suffix rather than printing "-0".
func TicketChannelName(name string, ticketID int) string {
	tail := ""
	if ticketID > 0 {
		tail = "-" + strconv.Itoa(ticketID)
	}
	// The extra byte is the hyphen that would join the head to the base.
	room := ChannelNameMax - len(ticketNameHead) - len(tail) - 1
	base := trimBase(SanitizeChannelName(name), room)
	if base == "" {
		return ticketNameHead + tail
	}
	return ticketNameHead + "-" + base + tail
}

// trimBase cuts the middle segment to room bytes on a hyphen-free boundary, so
// truncation never leaves the name ending in the separator Discord would then
// have to strip itself.
func trimBase(base string, room int) string {
	if room <= 0 {
		return ""
	}
	if len(base) > room {
		base = base[:room]
	}
	return strings.Trim(base, "-")
}
