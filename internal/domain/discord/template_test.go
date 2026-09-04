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

// The role set is a product decision, so it is pinned rather than left to
// drift: the fill creates these and only these, in this order, and the order
// is also the visual hierarchy Discord renders from.
func TestCommunityRolesAreTheAgreedSet(t *testing.T) {
	want := []string{"Owner", "Lead Mod", "Mods", "Regulars", "Member"}
	got := CommunityRoles()
	if len(got) != len(want) {
		t.Fatalf("roles = %d, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("role[%d] = %q, want %q (order is the hierarchy)", i, got[i].Name, name)
		}
	}
}

// Discord renders a member in their highest COLOURED role, so two staff
// tiers sharing a colour would be indistinguishable in the only place the
// colour appears.
func TestCommunityRoleColorsAreDistinct(t *testing.T) {
	seen := map[int]string{}
	for _, r := range CommunityRoles() {
		if r.Color == 0 {
			continue // uncoloured by design; see RoleSpec.Color
		}
		if prev, dup := seen[r.Color]; dup {
			t.Fatalf("%q and %q share colour %#06x", prev, r.Name, r.Color)
		}
		seen[r.Color] = r.Name
	}
	if len(seen) != 4 {
		t.Fatalf("coloured roles = %d, want 4 (Member is deliberately uncoloured)", len(seen))
	}
}

// Member is held by everyone. A colour there would win the name colour for
// anyone whose only other roles are uncoloured, flattening the hierarchy.
func TestMemberRoleHasNoColor(t *testing.T) {
	for _, r := range CommunityRoles() {
		if r.Name == "Member" && r.Color != 0 {
			t.Fatalf("Member colour = %#06x, want none", r.Color)
		}
	}
}
