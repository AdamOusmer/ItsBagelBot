// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "testing"

func TestCommunityTemplateBindsRequiredChannels(t *testing.T) {
	binds := map[string]bool{}
	for _, ch := range CommunityChannels() {
		if ch.Bind != "" {
			binds[ch.Bind] = true
		}
	}
	for _, want := range []string{"live", "clips", "welcome", "voice", "logs", "tickets", "ticketcat"} {
		if !binds[want] {
			t.Fatalf("template missing bind %q", want)
		}
	}
}

func TestCommunityTemplateHasVoiceHubAndStaff(t *testing.T) {
	var voiceHub, staff bool
	for _, ch := range CommunityChannels() {
		if ch.Bind == "voice" && ch.Type == ChannelVoice {
			voiceHub = true
		}
		if ch.Staff {
			staff = true
		}
	}
	if !voiceHub || !staff {
		t.Fatalf("voiceHub=%v staff=%v", voiceHub, staff)
	}
}

func TestBotPermissionsExcludeAdministrator(t *testing.T) {
	const administrator = 8
	if BotPermissions&administrator != 0 {
		t.Fatal("bot must not request Administrator")
	}
}

func TestBotPermissionsIncludeModeration(t *testing.T) {
	const (
		kick            = 2
		ban             = 4
		manageMessages  = 8192
		moderateMembers = 1 << 40
	)
	if BotPermissions&kick == 0 || BotPermissions&ban == 0 {
		t.Fatal("bot must request kick and ban")
	}
	if BotPermissions&manageMessages == 0 {
		t.Fatal("bot must request manage messages")
	}
	if BotPermissions&moderateMembers == 0 {
		t.Fatal("bot must request timeout (MODERATE_MEMBERS)")
	}
}
