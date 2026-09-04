// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"strings"
	"testing"
)

func TestCategoryAllowed(t *testing.T) {
	c := Config{CategoryAllow: "Minecraft, Valorant", CategoryDeny: "Just Chatting"}
	if !c.CategoryAllowed("Minecraft") {
		t.Fatal("allow-list should accept Minecraft")
	}
	if c.CategoryAllowed("Fortnite") {
		t.Fatal("allow-list should reject Fortnite")
	}
	if c.CategoryAllowed("Just Chatting") {
		t.Fatal("deny wins even if also allowed")
	}
	open := Config{}
	if !open.CategoryAllowed("anything") {
		t.Fatal("empty allow is every category")
	}
}

func TestTogglesDefaultOnExceptNoisyOnes(t *testing.T) {
	var c Config
	toggles := map[string]struct {
		on   func() bool
		want bool
	}{
		"live":    {c.LiveOn, true},
		"clips":   {c.ClipsOn, true},
		"welcome": {c.WelcomeOn, true},
		"voice":   {c.VoiceOn, true},
		"tickets": {c.TicketsOn, true},
		"logs":    {c.LogsOn, true},
		"levels":  {c.LevelsOn, true},
		"goodbye": {c.GoodbyeOn, false},
	}
	for name, tc := range toggles {
		if got := tc.on(); got != tc.want {
			t.Errorf("%s default = %v, want %v", name, got, tc.want)
		}
	}
}

func TestInviteAndTemplateURLs(t *testing.T) {
	if InviteURL("", "") != "" {
		t.Fatal("empty client id must yield no invite")
	}
	u := InviteURL("123", "")
	if u == "" {
		t.Fatal("invite url must not be empty")
	}
	// 1102012607574 = the old 1101945498710 plus CHANGE_NICKNAME (1<<26),
	// which the bot needs to rename ITSELF per guild for the premium
	// identity. Changing this number is a migration, not a rollout: Discord
	// freezes permissions into the bot's role at install, so existing guilds
	// keep the old grant until they re-authorize.
	if !strings.Contains(u, "permissions=1102012607574") {
		t.Fatalf("invite permissions drifted (keep in sync with dashboard DISCORD_BOT_PERMISSIONS): %q", u)
	}
	if !strings.Contains(u, "scope=bot") {
		t.Fatalf("invite = %q missing scope", u)
	}
	if TemplateURL("") != "" {
		t.Fatal("empty template code must yield no url")
	}
	if TemplateURL("abc") != "https://discord.new/abc" {
		t.Fatal("template url")
	}
}

// Default semantics are the thing most easily broken by a refactor: a
// default-ON toggle reads on for BOTH "" and anything that is not "off",
// while a default-OFF one demands the literal "on".
func TestNewToggleDefaults(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset is on", "", true},
		{"on is on", "on", true},
		{"off is off", "off", false},
		{"garbage is on (alertOn semantics)", "yes", true},
	}
	for _, tc := range cases {
		t.Run("transcript/"+tc.name, func(t *testing.T) {
			if got := (Config{TicketTranscriptEnabled: tc.value}).TicketTranscriptOn(); got != tc.want {
				t.Fatalf("TicketTranscriptOn(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
		t.Run("autorole/"+tc.name, func(t *testing.T) {
			if got := (Config{AutoRoleEnabled: tc.value}).AutoRoleOn(); got != tc.want {
				t.Fatalf("AutoRoleOn(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
	// The default-OFF pair must NOT have drifted to alertOn semantics.
	if (Config{GoodbyeEnabled: "yes"}).GoodbyeOn() {
		t.Fatal("GoodbyeOn must require the literal \"on\"")
	}
	if (Config{SubscribersEnabled: ""}).SubscribersOn() {
		t.Fatal("SubscribersOn must default off")
	}
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
		if got := (Config{TicketOpenLimit: raw}).TicketOpenLimitN(); got != want {
			t.Fatalf("TicketOpenLimitN(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestTicketPanelFillsDefaults(t *testing.T) {
	got := Config{}.TicketPanel()
	want := TicketPanelSpec{
		Title: TicketPanelTitleDefault, Body: TicketPanelBodyDefault,
		Button: TicketPanelButtonDefault, Color: LiveColor,
	}
	if got != want {
		t.Fatalf("panel = %+v, want %+v", got, want)
	}
	custom := Config{
		TicketPanelTitle: " Support ", TicketPanelBody: "Ask us.",
		TicketPanelButton: "Ask", TicketPanelColor: "#00FF80",
	}.TicketPanel()
	if custom.Title != "Support" || custom.Body != "Ask us." || custom.Button != "Ask" {
		t.Fatalf("panel = %+v", custom)
	}
	if custom.Color != 0x00FF80 {
		t.Fatalf("color = %#x, want 0x00ff80", custom.Color)
	}
	// An unparseable colour keeps the brand colour rather than rendering
	// black, which is what a zero would look like in Discord.
	if bad := (Config{TicketPanelColor: "nope"}).TicketPanel(); bad.Color != LiveColor {
		t.Fatalf("color = %#x, want LiveColor", bad.Color)
	}
}

func TestTicketStaffAndLogFallbacks(t *testing.T) {
	base := Config{OwnerRoleID: "o", LeadModRoleID: "l", ModsRoleID: "m", LogChannelID: "log"}
	if got := base.TicketStaffRoleIDs(); len(got) != 3 || got[0] != "o" {
		t.Fatalf("staff = %v, want the StaffRoleIDs fallback", got)
	}
	if got := base.TicketLogChannel(); got != "log" {
		t.Fatalf("log channel = %q, want the general fallback", got)
	}
	withOwn := base
	withOwn.TicketStaffRoles = "h1, h2"
	withOwn.TicketLogChannelID = "tickets-log"
	if got := withOwn.TicketStaffRoleIDs(); len(got) != 2 || got[1] != "h2" {
		t.Fatalf("staff = %v, want the explicit list", got)
	}
	if got := withOwn.TicketLogChannel(); got != "tickets-log" {
		t.Fatalf("log channel = %q, want the explicit one", got)
	}
}

func TestTierRoomsRoundTripsThroughParse(t *testing.T) {
	raw := []byte(`{"subsChannelId":"1","subsCategoryId":"2","vipChannelId":"3","vipCategoryId":"4"}`)
	got := Parse(raw).TierRooms()
	want := TierRooms{SubsChannelID: "1", SubsCategoryID: "2", VIPChannelID: "3", VIPCategoryID: "4"}
	if got != want {
		t.Fatalf("rooms = %+v, want %+v", got, want)
	}
}
