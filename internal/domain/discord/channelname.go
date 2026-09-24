// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strconv"
	"strings"
)

const ChannelNameMax = 100

const ticketNameHead = "ticket"
const separatorHyphenLen = 1

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

func allowedNameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func TicketChannelName(name string, ticketID int) string {
	tail := ""
	if ticketID > 0 {
		tail = "-" + strconv.Itoa(ticketID)
	}
	room := ChannelNameMax - len(ticketNameHead) - len(tail) - separatorHyphenLen
	base := trimBase(SanitizeChannelName(name), room)
	if base == "" {
		return ticketNameHead + tail
	}
	return ticketNameHead + "-" + base + tail
}

func trimBase(base string, room int) string {
	if room <= 0 {
		return ""
	}
	if len(base) > room {
		base = base[:room]
	}
	return strings.Trim(base, "-")
}
