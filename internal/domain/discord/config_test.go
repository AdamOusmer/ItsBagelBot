// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func wantToggle(onByDefault bool, value string) bool {
	return value == "on" || (onByDefault && value != "off")
}

func TestSettingToggleDefaults(t *testing.T) {
	for _, value := range []string{"", "on", "off", "yes"} {
		assert.Equal(t, wantToggle(true, value), TicketSettings{TicketTranscriptEnabled: value}.TranscriptOn(), "transcript %q", value)
		assert.Equal(t, wantToggle(true, value), LogsSettings{LogIgnoreBots: value}.IgnoreBotsOn(), "ignore bots %q", value)
	}
}

func TestCategoryAllowed(t *testing.T) {
	cases := []struct {
		name     string
		cfg      Config
		category string
		want     bool
	}{
		{"allow-list accepts a listed category", Config{CategoryAllow: "Minecraft, Valorant", CategoryDeny: "Just Chatting"}, "Minecraft", true},
		{"allow-list rejects an unlisted category", Config{CategoryAllow: "Minecraft, Valorant", CategoryDeny: "Just Chatting"}, "Fortnite", false},
		{"deny wins even if also allowed", Config{CategoryAllow: "Just Chatting", CategoryDeny: "Just Chatting"}, "Just Chatting", false},
		{"empty allow is every category", Config{}, "anything", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.live().CategoryAllowed(tc.category))
		})
	}
}

func TestInviteAndTemplateURLs(t *testing.T) {
	assert.Empty(t, InviteURL("", ""), "empty client id must yield no invite")
	assert.Empty(t, TemplateURL(""), "empty template code must yield no url")
	assert.Equal(t, "https://discord.new/abc", TemplateURL("abc"))
	invite := InviteURL("123", "")
	assert.Contains(t, invite, "permissions=1102012607702", "keep in sync with dashboard DISCORD_BOT_PERMISSIONS")
	assert.Contains(t, invite, "scope=bot")
}

func TestTicketOpenLimitNClamps(t *testing.T) {
	cases := map[string]int{
		"":     TicketOpenLimitDefault,
		"1":    1,
		"3":    3,
		"5":    5,
		"9":    TicketOpenLimitMax,
		"0":    TicketOpenLimitDefault,
		"-2":   TicketOpenLimitDefault,
		"many": TicketOpenLimitDefault,
		" 4 ":  4,
	}
	for raw, want := range cases {
		assert.Equal(t, want, TicketSettings{TicketOpenLimit: raw}.OpenLimitN(), "limit %q", raw)
	}
}

type panelWant struct {
	title  string
	body   string
	button string
	color  int
}

func wantPanel(t *testing.T, got TicketPanelSpec, want panelWant) {
	t.Helper()
	assert.Equal(t, want, panelWant{got.Title, got.Body, got.Button, got.ColorOr(0)})
}

func TestTicketPanelFillsDefaults(t *testing.T) {
	wantPanel(t, Config{}.TicketPanel(), panelWant{
		title: TicketPanelTitleDefault, body: TicketPanelBodyDefault,
		button: TicketPanelButtonDefault, color: LiveColor,
	})
	custom := Config{
		TicketPanelTitle: " Support ", TicketPanelBody: "Ask us.",
		TicketPanelButton: "Ask", TicketPanelColor: "#00FF80",
	}.TicketPanel()
	wantPanel(t, custom, panelWant{title: "Support", body: "Ask us.", button: "Ask", color: 0x00FF80})
	assert.Equal(t, LiveColor, Config{TicketPanelColor: "nope"}.TicketPanel().ColorOr(0))
}

func TestTicketPanelSpecOrDefaultsFillsBlanks(t *testing.T) {
	wantPanel(t, TicketPanelSpec{Title: "Kept"}.OrDefaults(), panelWant{
		title: "Kept", body: TicketPanelBodyDefault,
		button: TicketPanelButtonDefault, color: LiveColor,
	})
}

func TestTicketPanelSpecKeepsABlackColour(t *testing.T) {
	black := 0
	got := TicketPanelSpec{Color: &black}.OrDefaults()
	if got.ColorOr(LiveColor) != 0 {
		t.Fatalf("color = %#x, want #000000 to survive OrDefaults", got.ColorOr(LiveColor))
	}
	if e := TicketPanelEmbed(got); e.Color != 0 {
		t.Fatalf("embed color = %#x, want the black the streamer picked", e.Color)
	}

	unset := TicketPanelSpec{}.OrDefaults()
	if unset.Color == nil || *unset.Color != LiveColor {
		t.Fatalf("unset color = %v, want the brand default", unset.Color)
	}

	fromConfig := Config{TicketPanelColor: "#000000"}.TicketPanel()
	if fromConfig.ColorOr(LiveColor) != 0 {
		t.Fatalf("config color = %#x, want 0", fromConfig.ColorOr(LiveColor))
	}
}

func TestTicketStaffAndLogFallbacks(t *testing.T) {
	base := Config{OwnerRoleID: "o", LeadModRoleID: "l", ModsRoleID: "m", LogChannelID: "log"}
	withOwn := base
	withOwn.TicketStaffRoles = "h1, h2"
	withOwn.TicketLogChannelID = "tickets-log"

	assert.Equal(t, []string{"o", "l", "m"}, base.tickets().StaffRoleIDs(base.roles()), "the StaffRoleIDs fallback")
	assert.Equal(t, "log", base.tickets().LogChannel(base.logs()), "the general log fallback")
	assert.Equal(t, []string{"h1", "h2"}, withOwn.tickets().StaffRoleIDs(withOwn.roles()), "the explicit list")
	assert.Equal(t, "tickets-log", withOwn.tickets().LogChannel(withOwn.logs()), "the explicit channel")
}

func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"#ffffff", 0xffffff, true},
		{"  #0A0b0C ", 0x0a0b0c, true},
		{"#000000", 0, true},
		{"#fff", 0, false},
		{"ffffff", 0, false},
		{"#1ffffff", 0, false},
		{"#gggggg", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseHexColor(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseHexColor(%q) = (%#x, %v), want (%#x, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLogCategoryOnFollowsItsSwitch(t *testing.T) {
	cases := []struct {
		cat LogCategory
		set func(*Config, string)
	}{
		{LogMessages, func(c *Config, v string) { c.LogMessagesEnabled = v }},
		{LogMembers, func(c *Config, v string) { c.LogMembersEnabled = v }},
		{LogVoice, func(c *Config, v string) { c.LogVoiceEnabled = v }},
		{LogModeration, func(c *Config, v string) { c.LogModerationEnabled = v }},
		{LogChannels, func(c *Config, v string) { c.LogChannelsEnabled = v }},
		{LogRoles, func(c *Config, v string) { c.LogRolesEnabled = v }},
		{LogServer, func(c *Config, v string) { c.LogServerEnabled = v }},
	}
	for _, tc := range cases {
		t.Run(string(tc.cat), func(t *testing.T) {
			var c Config
			assert.True(t, c.logs().CategoryOn(tc.cat), "blank means on")
			tc.set(&c, "off")
			assert.False(t, c.logs().CategoryOn(tc.cat))
		})
	}
	assert.False(t, LogsSettings{}.CategoryOn("bogus"))
}

func TestLogChannelForFallsBackToTheGeneralChannel(t *testing.T) {
	l := LogsSettings{LogChannelID: "general", LogVoiceChannelID: " voice "}

	assert.Equal(t, "voice", l.ChannelFor(LogVoice))
	assert.Equal(t, "general", l.ChannelFor(LogMessages))
	assert.Equal(t, "general", l.ChannelFor(LogRoles), "categories without their own channel use the general one")
	assert.Empty(t, LogsSettings{}.ChannelFor(LogVoice))
}

func TestLogIgnores(t *testing.T) {
	l := LogsSettings{LogIgnoredChannels: "10, 20,,30"}

	assert.True(t, l.Ignores("20"))
	assert.False(t, l.Ignores("40"))
	assert.False(t, l.Ignores(""))
	assert.False(t, LogsSettings{}.Ignores("20"))
}

func TestVoiceName(t *testing.T) {
	long := strings.Repeat("é", VoiceNameMax+5)
	cases := []struct{ name, tpl, want string }{
		{"blank uses the owner", "", "Ada"},
		{"token replaced", "{owner}'s room", "Ada's room"},
		{"no token kept literal", "Lounge", "Lounge"},
		{"blank after trim uses the owner", "   ", "Ada"},
		{"clipped to the max in runes", long, strings.Repeat("é", VoiceNameMax)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, VoiceSettings{VoiceNameTemplate: tc.tpl}.Name("Ada"))
		})
	}
}

func TestVoiceCategorySet(t *testing.T) {
	assert.False(t, VoiceSettings{}.CategorySet())
	assert.False(t, VoiceSettings{VoiceCategoryID: "  "}.CategorySet())
	assert.True(t, VoiceSettings{VoiceCategoryID: "c1"}.CategorySet())
}

func TestVoiceLimitAndPrivacyDefaults(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "0": 0, "7": 7, "99": 99, "100": 0, "-1": 0, "x": 0} {
		assert.Equal(t, want, VoiceSettings{VoiceUserLimit: raw}.Limit(), "limit %q", raw)
	}
	for raw, want := range map[string]string{"": "open", "open": "open", "locked": "locked", "hidden": "hidden", "bogus": "open"} {
		assert.Equal(t, want, VoiceSettings{VoicePrivacyMode: raw}.Privacy(), "privacy %q", raw)
	}
}
