// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodID = "123456789012345678"

func TestValidateConfigAcceptsEmptyAndFilled(t *testing.T) {
	full := Config{
		GuildID: goodID, ModsRoleID: goodID, TicketArchiveCategoryID: goodID,
		TicketStaffRoles: goodID + "," + goodID, TicketOpenLimit: "3",
		TicketPanelColor: "#C47A3A", TicketPanelTitle: "Help", TicketPanelButton: "Open",
		LogIgnoredChannels: goodID + "," + goodID, VoiceNameTemplate: "{owner}", VoiceUserLimit: "0", VoicePrivacyMode: "hidden",
		LogVoiceChannelID: goodID, VoiceCategoryID: goodID, LogIgnoreBots: "off", LogRolesEnabled: "on",
		PinnedRoles: "mods=" + goodID, AutoRoleEnabled: "off", TicketTranscriptEnabled: "on",
		TicketPanelBody: strings.Repeat("é", TicketPanelBodyMax),
	}

	assert.Empty(t, ValidateConfig(Config{}), "a blank config must be storable")
	assert.Empty(t, ValidateConfig(full), "a valid config (with a body of exactly the max in runes) was rejected")
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
		{"log category toggle", Config{LogVoiceEnabled: "maybe"}, "logVoiceEnabled", CodeInvalidFlag},
		{"ignore bots toggle", Config{LogIgnoreBots: "1"}, "logIgnoreBots", CodeInvalidFlag},
		{"per category log channel", Config{LogModerationChannelID: "mod-log"}, "logModerationChannelId", CodeInvalidID},
		{"ignored channel list", Config{LogIgnoredChannels: goodID + ",nope"}, "logIgnoredChannelIds", CodeInvalidID},
		{"voice category", Config{VoiceCategoryID: "cat"}, "voiceCategoryId", CodeInvalidID},
		{"voice name too long", Config{VoiceNameTemplate: strings.Repeat("x", VoiceNameMax+1)}, "voiceNameTemplate", CodeTooLong},
		{"voice limit above max", Config{VoiceUserLimit: "100"}, "voiceUserLimit", CodeInvalidRange},
		{"voice limit negative", Config{VoiceUserLimit: "-1"}, "voiceUserLimit", CodeInvalidRange},
		{"voice limit signed", Config{VoiceUserLimit: "+5"}, "voiceUserLimit", CodeInvalidRange},
		{"voice privacy unknown", Config{VoicePrivacyMode: "secret"}, "voicePrivacy", CodeInvalidChoice},
		{"new toggle is validated too", Config{AutoRoleEnabled: "true"}, "autoRoleEnabled", CodeInvalidFlag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateConfig(tc.cfg)
			require.Len(t, errs, 1)
			assert.Equal(t, tc.field, errs[0].Field)
			assert.Equal(t, tc.code, errs[0].Code)
		})
	}
}

func TestValidateConfigOrderIsStable(t *testing.T) {
	cfg := Config{LiveChannelID: "bad", GuildID: "bad", ModsRoleID: "bad"}

	first := ValidateConfig(cfg)

	require.Len(t, first, 3)
	assert.Equal(t, "guildId", first[0].Field, "errors come back alphabetical")
	for i := 0; i < 20; i++ {
		assert.True(t, slices.Equal(first, ValidateConfig(cfg)), "run %d differs", i)
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
		assert.Equal(t, want, ValidSnowflake(id), "ValidSnowflake(%q)", id)
	}
}

func TestSanitizeConfig(t *testing.T) {
	t.Run("zeroes only the invalid fields", func(t *testing.T) {
		cfg := Config{
			GuildID:          "100000000000000001",
			LiveChannelID:    "100000000000000002",
			ClipsChannelID:   "#clips",
			TicketPanelColor: "burgundy",
			TicketOpenLimit:  "9",
			LiveEnabled:      "maybe",
		}

		clean, bad := SanitizeConfig(cfg)

		assert.Len(t, bad, 4)
		assert.Equal(t, Config{GuildID: cfg.GuildID, LiveChannelID: cfg.LiveChannelID}, clean)
	})

	t.Run("zeroed fields fall back to their defaults", func(t *testing.T) {
		clean, _ := SanitizeConfig(Config{TicketOpenLimit: "12", TicketTranscriptEnabled: "yes"})

		assert.Equal(t, TicketOpenLimitDefault, clean.TicketOpenLimitN())
		assert.True(t, clean.TicketTranscriptOn(), "a zeroed transcript flag falls back to its default-ON reader")
	})

	t.Run("leaves a valid config alone", func(t *testing.T) {
		cfg := Config{GuildID: "100000000000000001", TicketPanelColor: "#ff8800", LiveEnabled: "off"}

		clean, bad := SanitizeConfig(cfg)

		assert.Empty(t, bad)
		assert.Equal(t, cfg, clean)
	})
}

func TestEveryValidatedFieldNameExistsOnConfig(t *testing.T) {
	bad := ValidateConfig(Config{
		GuildID: "x", ClipsChannelID: "x", TicketStaffRoles: "x", TicketPanelColor: "x",
		TicketOpenLimit: "x", PinnedRoles: "nope", LiveEnabled: "x",
		TicketPanelTitle: strings.Repeat("t", TicketPanelTitleMax+1),
	})
	require.NotEmpty(t, bad, "the fixture was supposed to be invalid")

	tags := configJSONTags(t)
	for _, fe := range bad {
		assert.Contains(t, tags, fe.Field, "ValidateConfig reports %q, which is not a Config json tag", fe.Field)
	}
}
