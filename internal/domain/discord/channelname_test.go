// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var channelNameShape = regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*)?$`)

func TestSanitizeChannelName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "lowercases", in: "Ada", want: "ada"},
		{name: "spaces collapse to one hyphen", in: "Ada  Lovelace", want: "ada-lovelace"},
		{name: "repeated separators collapse", in: "a___-__b", want: "a-b"},
		{name: "leading and trailing separators are dropped", in: "--ada--", want: "ada"},
		{name: "punctuation is a separator", in: "ada.lovelace!", want: "ada-lovelace"},
		{name: "digits survive", in: "User1234", want: "user1234"},
		{name: "accents are separators, not letters", in: "Ünïcödé", want: "n-c-d"},
		{name: "cyrillic yields nothing usable", in: "Пользователь", want: ""},
		{name: "emoji only", in: "🍩🍩", want: ""},
		{name: "mixed script keeps the latin run", in: "ada 日本", want: "ada"},
		{name: "only separators", in: "___", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeChannelName(tc.in)
			assert.Equal(t, tc.want, got)
			assert.Regexp(t, channelNameShape, got, "output must be a Discord-acceptable channel name")
		})
	}
}

func TestTicketChannelName(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		ticketID int
		want     string
	}{
		{name: "row id is the suffix", base: "Ada", ticketID: 41, want: "ticket-ada-41"},
		{name: "no id yet", base: "Ada", ticketID: 0, want: "ticket-ada"},
		{name: "negative id is no id", base: "Ada", ticketID: -3, want: "ticket-ada"},
		{name: "unusable name falls back to the bare head", base: "Пользователь", ticketID: 7, want: "ticket-7"},
		{name: "nothing at all", base: "", ticketID: 0, want: "ticket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, TicketChannelName(tc.base, tc.ticketID))
		})
	}
}

func TestTicketChannelNameStaysUnderTheCeiling(t *testing.T) {
	got := TicketChannelName(strings.Repeat("ada ", 60), 123456)

	assert.LessOrEqual(t, len(got), ChannelNameMax)
	assert.True(t, strings.HasSuffix(got, "-123456"), "the row id is preserved: %q", got)
	assert.Regexp(t, channelNameShape, got, "no dangling separator")
}
