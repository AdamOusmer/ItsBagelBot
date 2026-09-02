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
	for _, want := range []string{"live", "clips", "welcome", "alerts", "voice"} {
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
