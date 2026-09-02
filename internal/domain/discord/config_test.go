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

func TestAlertsChannelFallsBackToLive(t *testing.T) {
	c := Config{LiveChannelID: "live-1"}
	if c.AlertsChannel() != "live-1" {
		t.Fatalf("got %q", c.AlertsChannel())
	}
	c.AlertsChannelID = "alerts-1"
	if c.AlertsChannel() != "alerts-1" {
		t.Fatalf("got %q", c.AlertsChannel())
	}
}

func TestTogglesDefaultOnExceptNoisyOnes(t *testing.T) {
	var c Config
	toggles := map[string]struct {
		on   func() bool
		want bool
	}{
		"live":      {c.LiveOn, true},
		"clips":     {c.ClipsOn, true},
		"raid":      {c.RaidOn, true},
		"welcome":   {c.WelcomeOn, true},
		"voice":     {c.VoiceOn, true},
		"cheer":     {c.CheerOn, false},
		"goodbye":   {c.GoodbyeOn, false},
		"milestone": {c.SubMilestoneOn, false},
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
	if u == "" || !strings.Contains(u, "permissions=") || !strings.Contains(u, "scope=bot") {
		t.Fatalf("invite = %q", u)
	}
	if TemplateURL("") != "" || TemplateURL("abc") != "https://discord.new/abc" {
		t.Fatal("template url")
	}
}
