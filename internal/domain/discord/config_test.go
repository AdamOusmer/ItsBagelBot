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
	if !strings.Contains(u, "permissions=1101945498710") {
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
