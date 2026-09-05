// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"reflect"
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
		{"pinned pair without a separator", Config{PinnedRoles: goodID}, "pinnedRoles", CodeMalformedPair},
		{"pinned pair with an empty id", Config{PinnedRoles: "mods="}, "pinnedRoles", CodeMalformedPair},
		{"pinned pair with an empty slot", Config{PinnedRoles: "=" + goodID}, "pinnedRoles", CodeMalformedPair},
		{"same slot pinned twice", Config{PinnedRoles: "mods=" + goodID + ",mods=" + goodID}, "pinnedRoles", CodeDuplicateSlot},
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

// Sanitize keeps a config usable: the bad field goes, everything around it
// stays. Refusing the whole blob instead would take a guild's live alerts
// down over a mistyped ticket colour.
func TestSanitizeConfigZeroesOnlyTheInvalidFields(t *testing.T) {
	cfg := Config{
		GuildID:          "100000000000000001",
		LiveChannelID:    "100000000000000002",
		ClipsChannelID:   "#clips",
		TicketPanelColor: "burgundy",
		TicketOpenLimit:  "9",
		LiveEnabled:      "maybe",
	}

	clean, bad := SanitizeConfig(cfg)

	if len(bad) != 4 {
		t.Fatalf("errors = %+v, want 4", bad)
	}
	if clean.ClipsChannelID != "" || clean.TicketPanelColor != "" ||
		clean.TicketOpenLimit != "" || clean.LiveEnabled != "" {
		t.Fatalf("clean = %+v, want every rejected field zeroed", clean)
	}
	if clean.GuildID != cfg.GuildID || clean.LiveChannelID != cfg.LiveChannelID {
		t.Fatalf("clean = %+v, want the valid fields untouched", clean)
	}
}

// A zeroed field must read as UNSET, not as a broken value: every reader
// documents a default for empty, so the guild keeps working.
func TestSanitizeConfigLeavesZeroedFieldsOnTheirDefaults(t *testing.T) {
	clean, _ := SanitizeConfig(Config{TicketOpenLimit: "12", TicketTranscriptEnabled: "yes"})

	if clean.TicketOpenLimitN() != TicketOpenLimitDefault {
		t.Fatalf("limit = %d, want the default %d", clean.TicketOpenLimitN(), TicketOpenLimitDefault)
	}
	if !clean.TicketTranscriptOn() {
		t.Fatal("a zeroed transcript flag must fall back to its default-ON reader")
	}
}

func TestSanitizeConfigLeavesAValidConfigAlone(t *testing.T) {
	cfg := Config{GuildID: "100000000000000001", TicketPanelColor: "#ff8800", LiveEnabled: "off"}

	clean, bad := SanitizeConfig(cfg)

	if len(bad) != 0 {
		t.Fatalf("errors = %+v, want none", bad)
	}
	if clean != cfg {
		t.Fatalf("clean = %+v, want it unchanged", clean)
	}
}

// The zeroing walks Config by JSON tag, so a field whose tag ValidateConfig
// names must actually exist under that tag. A drifted tag would report an
// error nothing then clears.
func TestEveryValidatedFieldNameExistsOnConfig(t *testing.T) {
	bad := ValidateConfig(Config{
		GuildID: "x", ClipsChannelID: "x", TicketStaffRoles: "x", TicketPanelColor: "x",
		TicketOpenLimit: "x", PinnedRoles: "nope", LiveEnabled: "x",
		TicketPanelTitle: strings.Repeat("t", TicketPanelTitleMax+1),
	})
	if len(bad) == 0 {
		t.Fatal("the fixture was supposed to be invalid")
	}
	tags := configFieldsByTag(reflect.TypeOf(Config{}))
	for _, fe := range bad {
		if _, ok := tags[fe.Field]; !ok {
			t.Fatalf("ValidateConfig reports %q, which is not a Config json tag", fe.Field)
		}
	}
}
