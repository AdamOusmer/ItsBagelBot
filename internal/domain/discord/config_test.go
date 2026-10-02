// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func wantToggle(onByDefault bool, value string) bool {
	return value == "on" || (onByDefault && value != "off")
}

func TestToggleDefaults(t *testing.T) {
	toggles := []struct {
		name        string
		set         func(*Config, string)
		read        func(Config) bool
		onByDefault bool
	}{
		{"live", func(c *Config, v string) { c.LiveEnabled = v }, Config.LiveOn, true},
		{"clips", func(c *Config, v string) { c.ClipsEnabled = v }, Config.ClipsOn, true},
		{"welcome", func(c *Config, v string) { c.WelcomeEnabled = v }, Config.WelcomeOn, true},
		{"voice", func(c *Config, v string) { c.VoiceEnabled = v }, Config.VoiceOn, true},
		{"tickets", func(c *Config, v string) { c.TicketsEnabled = v }, Config.TicketsOn, true},
		{"logs", func(c *Config, v string) { c.LogsEnabled = v }, Config.LogsOn, true},
		{"levels", func(c *Config, v string) { c.LevelsEnabled = v }, Config.LevelsOn, true},
		{"ticket transcript", func(c *Config, v string) { c.TicketTranscriptEnabled = v }, Config.TicketTranscriptOn, true},
		{"autorole", func(c *Config, v string) { c.AutoRoleEnabled = v }, Config.AutoRoleOn, true},
		{"goodbye", func(c *Config, v string) { c.GoodbyeEnabled = v }, Config.GoodbyeOn, false},
		{"subscribers", func(c *Config, v string) { c.SubscribersEnabled = v }, Config.SubscribersOn, false},
		{"link guard", func(c *Config, v string) { c.LinkGuardEnabled = v }, Config.LinkGuardOn, false},
	}
	for _, tc := range toggles {
		for _, value := range []string{"", "on", "off", "yes"} {
			t.Run(tc.name+"/"+value, func(t *testing.T) {
				var c Config
				tc.set(&c, value)
				assert.Equal(t, wantToggle(tc.onByDefault, value), tc.read(c))
			})
		}
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
			assert.Equal(t, tc.want, tc.cfg.CategoryAllowed(tc.category))
		})
	}
}

func TestInviteAndTemplateURLs(t *testing.T) {
	assert.Empty(t, InviteURL("", ""), "empty client id must yield no invite")
	assert.Empty(t, TemplateURL(""), "empty template code must yield no url")
	assert.Equal(t, "https://discord.new/abc", TemplateURL("abc"))
	invite := InviteURL("123", "")
	assert.Contains(t, invite, "permissions=1102012607574", "keep in sync with dashboard DISCORD_BOT_PERMISSIONS")
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
		assert.Equal(t, want, Config{TicketOpenLimit: raw}.TicketOpenLimitN(), "limit %q", raw)
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

	assert.Equal(t, []string{"o", "l", "m"}, base.TicketStaffRoleIDs(), "the StaffRoleIDs fallback")
	assert.Equal(t, "log", base.TicketLogChannel(), "the general log fallback")
	assert.Equal(t, []string{"h1", "h2"}, withOwn.TicketStaffRoleIDs(), "the explicit list")
	assert.Equal(t, "tickets-log", withOwn.TicketLogChannel(), "the explicit channel")
}

func TestTierRoomsRoundTripsThroughParse(t *testing.T) {
	raw := []byte(`{"subsChannelId":"1","subsCategoryId":"2","vipChannelId":"3","vipCategoryId":"4"}`)

	assert.Equal(t, TierRooms{SubsChannelID: "1", SubsCategoryID: "2", VIPChannelID: "3", VIPCategoryID: "4"}, Parse(raw).TierRooms())
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
