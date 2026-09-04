// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"
)

const goodID = "123456789012345678"

func TestValidateConfigAcceptsEmptyAndFilled(t *testing.T) {
	if errs := ValidateConfig(Config{}); len(errs) != 0 {
		t.Fatalf("a blank config must be storable, got %v", errs)
	}
	full := Config{
		GuildID: goodID, ModsRoleID: goodID, TicketArchiveCategoryID: goodID,
		TicketStaffRoles: goodID + "," + goodID, TicketOpenLimit: "3",
		TicketPanelColor: "#C47A3A", TicketPanelTitle: "Help", TicketPanelButton: "Open",
		PinnedRoles: "mods=" + goodID, AutoRoleEnabled: "off", TicketTranscriptEnabled: "on",
	}
	if errs := ValidateConfig(full); len(errs) != 0 {
		t.Fatalf("a valid config was rejected: %v", errs)
	}
}

func TestValidateConfigFieldErrors(t *testing.T) {
	cases := []struct {
		name  string
		cfg   Config
		field string
		code  string
	}{
		{"channel name pasted as id", Config{LiveChannelID: "now-live"}, "liveChannelId", CodeInvalidID},
		{"mention wrapper kept", Config{ModsRoleID: "<@&" + goodID + ">"}, "modsRoleId", CodeInvalidID},
		{"truncated id", Config{GuildID: "12345"}, "guildId", CodeInvalidID},
		{"bad ticket staff id", Config{TicketStaffRoles: "nope"}, "ticketStaffRoleIds", CodeInvalidID},
		{"three digit hex", Config{TicketPanelColor: "#abc"}, "ticketPanelColor", CodeInvalidColor},
		{"hex without hash", Config{TicketPanelColor: "C47A3A"}, "ticketPanelColor", CodeInvalidColor},
		{"non hex digits", Config{TicketPanelColor: "#zzzzzz"}, "ticketPanelColor", CodeInvalidColor},
		{"limit zero", Config{TicketOpenLimit: "0"}, "ticketOpenLimit", CodeInvalidRange},
		{"limit above max", Config{TicketOpenLimit: "6"}, "ticketOpenLimit", CodeInvalidRange},
		{"limit not a number", Config{TicketOpenLimit: "many"}, "ticketOpenLimit", CodeInvalidRange},
		{"title too long", Config{TicketPanelTitle: strings.Repeat("x", TicketPanelTitleMax+1)}, "ticketPanelTitle", CodeTooLong},
		{"body too long", Config{TicketPanelBody: strings.Repeat("x", TicketPanelBodyMax+1)}, "ticketPanelBody", CodeTooLong},
		{"button too long", Config{TicketPanelButton: strings.Repeat("x", TicketPanelButtonMax+1)}, "ticketPanelButton", CodeTooLong},
		{"unknown pinned slot", Config{PinnedRoles: "janitor=" + goodID}, "pinnedRoles", CodeInvalidSlot},
		{"pinned pair without a separator", Config{PinnedRoles: goodID}, "pinnedRoles", CodeInvalidSlot},
		{"pinned id is not a snowflake", Config{PinnedRoles: "mods=Mods"}, "pinnedRoles", CodeInvalidID},
		{"toggle is not on or off", Config{LevelsEnabled: "yes"}, "levelsEnabled", CodeInvalidFlag},
		{"new toggle is validated too", Config{AutoRoleEnabled: "true"}, "autoRoleEnabled", CodeInvalidFlag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateConfig(tc.cfg)
			if len(errs) != 1 {
				t.Fatalf("errors = %v, want exactly one", errs)
			}
			if errs[0].Field != tc.field || errs[0].Code != tc.code {
				t.Fatalf("error = %+v, want {%s %s}", errs[0], tc.field, tc.code)
			}
		})
	}
}

// A body of exactly the maximum is allowed; the limit is inclusive, and the
// count is in runes so an accented body is not rejected for its bytes.
func TestValidateConfigLengthBoundaryIsRunes(t *testing.T) {
	at := Config{TicketPanelBody: strings.Repeat("é", TicketPanelBodyMax)}
	if errs := ValidateConfig(at); len(errs) != 0 {
		t.Fatalf("a body of exactly the max was rejected: %v", errs)
	}
}

// Field errors must come back in a stable order or the dashboard's
// highlighting flickers between two identical saves.
func TestValidateConfigOrderIsStable(t *testing.T) {
	cfg := Config{LiveChannelID: "bad", GuildID: "bad", ModsRoleID: "bad"}
	first := ValidateConfig(cfg)
	for i := 0; i < 20; i++ {
		got := ValidateConfig(cfg)
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d differs: %v vs %v", i, got, first)
			}
		}
	}
	if first[0].Field != "guildId" {
		t.Fatalf("first error = %+v, want guildId (alphabetical)", first[0])
	}
}

func TestValidSnowflake(t *testing.T) {
	cases := map[string]bool{
		"":                      true,
		goodID:                  true,
		"12345678901234567890":  true,
		"1234567890123456":      false,
		"123456789012345678901": false,
		"12345678901234567a":    false,
		"-12345678901234567":    false,
	}
	for id, want := range cases {
		if got := ValidSnowflake(id); got != want {
			t.Fatalf("ValidSnowflake(%q) = %v, want %v", id, got, want)
		}
	}
}
