// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"
)

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeChannelName(tc.in); got != tc.want {
				t.Fatalf("SanitizeChannelName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeChannelNameOutputIsAlwaysAcceptable(t *testing.T) {
	inputs := []string{"Ada Lovelace", "Ünïcödé Tïckét", "🍩 donut 🍩", "___", "A-B_C.D"}
	for _, in := range inputs {
		wantAcceptableChannelName(t, in, SanitizeChannelName(in))
	}
}

// wantAcceptableChannelName holds the whole output contract in one place:
// Discord refuses a channel name outside [a-z0-9-], and a doubled or edge
// separator renders as a typo the streamer cannot fix from the dashboard.
func wantAcceptableChannelName(t *testing.T, in, got string) {
	t.Helper()
	if strings.Contains(got, "--") {
		t.Fatalf("SanitizeChannelName(%q) = %q: repeated separator", in, got)
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Fatalf("SanitizeChannelName(%q) = %q: edge separator", in, got)
	}
	wantChannelNameRunes(t, in, got)
}

// wantChannelNameRunes checks the alphabet, rune by rune.
func wantChannelNameRunes(t *testing.T, in, got string) {
	t.Helper()
	for _, r := range got {
		if !allowedNameRune(r) && r != '-' {
			t.Fatalf("SanitizeChannelName(%q) = %q: rune %q is outside [a-z0-9-]", in, got, r)
		}
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
			if got := TicketChannelName(tc.base, tc.ticketID); got != tc.want {
				t.Fatalf("TicketChannelName(%q, %d) = %q, want %q", tc.base, tc.ticketID, got, tc.want)
			}
		})
	}
}

// A long username must not push the name past Discord's 100-character ceiling,
// and the truncation must not leave the name ending in the separator.
func TestTicketChannelNameStaysUnderTheCeiling(t *testing.T) {
	long := strings.Repeat("ada ", 60) // 240 characters, 120 of them separators
	got := TicketChannelName(long, 123456)

	if len(got) > ChannelNameMax {
		t.Fatalf("name is %d characters: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "-123456") {
		t.Fatalf("name = %q, want the row id preserved", got)
	}
	if strings.Contains(got, "--") || strings.HasSuffix(strings.TrimSuffix(got, "-123456"), "-") {
		t.Fatalf("name = %q, want no dangling separator", got)
	}
}
