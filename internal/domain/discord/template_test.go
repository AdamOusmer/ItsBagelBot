// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"slices"
	"testing"
)

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
		if len(ch.AllowRoles) > 0 {
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

func TestCommunityRolesAreTheAgreedSet(t *testing.T) {
	want := []string{"Owner", "Lead Mod", "Mods", "VIP", "Subscriber", "Regulars", "Member"}
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

func TestCommunityRoleColorsAreDistinct(t *testing.T) {
	seen := map[int]string{}
	for _, r := range CommunityRoles() {
		if r.Color == 0 {
			continue
		}
		if prev, dup := seen[r.Color]; dup {
			t.Fatalf("%q and %q share colour %#06x", prev, r.Name, r.Color)
		}
		seen[r.Color] = r.Name
	}
	if len(seen) != 6 {
		t.Fatalf("coloured roles = %d, want 6 (Member is deliberately uncoloured)", len(seen))
	}
}

func TestMemberRoleHasNoColor(t *testing.T) {
	for _, r := range CommunityRoles() {
		if r.Name == "Member" && r.Color != 0 {
			t.Fatalf("Member colour = %#06x, want none", r.Color)
		}
	}
}

func TestLeadModHoldsAdministrator(t *testing.T) {
	var found bool
	for _, r := range CommunityRoles() {
		if r.Name != "Lead Mod" {
			continue
		}
		found = true
		if r.Permissions&PermAdministrator == 0 {
			t.Fatalf("Lead Mod permissions = %d, want Administrator", r.Permissions)
		}
	}
	if !found {
		t.Fatal("Lead Mod role missing from the template")
	}
}

func TestModsHoldModerationButNotAdministrator(t *testing.T) {
	for _, r := range CommunityRoles() {
		if r.Name != RoleMods {
			continue
		}
		if r.Permissions&PermAdministrator != 0 {
			t.Fatal("Mods holds Administrator, which erases the Lead Mod tier")
		}
		for _, want := range []int64{PermBanMembers, PermKickMembers, PermManageMessages, PermModerateMembers} {
			if r.Permissions&want == 0 {
				t.Fatalf("Mods is missing permission bit %d", want)
			}
		}
	}
}

func TestAccessTiersHoldNoPermissions(t *testing.T) {
	for _, r := range CommunityRoles() {
		switch r.Name {
		case RoleLeadMod, RoleMods:
			continue
		}
		if r.Permissions != 0 {
			t.Fatalf("%q permissions = %d, want 0", r.Name, r.Permissions)
		}
	}
}

func TestBotStillRefusesAdministratorItself(t *testing.T) {
	if BotPermissions&int(PermAdministrator) != 0 {
		t.Fatal("the bot invite now requests Administrator")
	}
}

func TestGatedChannelsNameRolesThatExist(t *testing.T) {
	created := map[string]bool{}
	for _, r := range CommunityRoles() {
		created[r.Name] = true
	}
	for _, ch := range CommunityChannels() {
		for _, name := range ch.AllowRoles {
			if !created[name] {
				t.Fatalf("channel %q gates on role %q, which the template never creates", ch.Name, name)
			}
		}
	}
}

func missingStaffRole(ch ChannelSpec) string {
	for _, staff := range StaffRoles {
		if !slices.Contains(ch.AllowRoles, staff) {
			return staff
		}
	}
	return ""
}

func TestStaffCanReadEveryGatedChannel(t *testing.T) {
	for _, ch := range CommunityChannels() {
		if len(ch.AllowRoles) == 0 {
			continue
		}
		if missing := missingStaffRole(ch); missing != "" {
			t.Fatalf("gated channel %q excludes staff role %q", ch.Name, missing)
		}
	}
}

func TestVIPAreaExcludesSubscribers(t *testing.T) {
	if slices.Contains(VIPRoles, RoleSubscriber) {
		t.Fatal("VIP area admits every subscriber, which defeats the tier")
	}
	if !slices.Contains(SubscriberRoles, RoleVIP) {
		t.Fatal("subscriber area excludes VIPs, who rank above them")
	}
}

func subscriberRoleCreatedWithTierOff() bool {
	for _, r := range CommunityRoles() {
		if r.Name == RoleSubscriber && FeatureEnabled(r.Feature, false) {
			return true
		}
	}
	return false
}

func subscriberGatedChannels(t *testing.T) int {
	t.Helper()
	var n int
	for _, ch := range CommunityChannels() {
		if ch.Feature != FeatureSubscribers {
			continue
		}
		n++
		if FeatureEnabled(ch.Feature, false) {
			t.Fatalf("channel %q is created even with the subscriber tier off", ch.Name)
		}
		if !FeatureEnabled(ch.Feature, true) {
			t.Fatalf("channel %q is never created even with the tier on", ch.Name)
		}
	}
	return n
}

func TestSubscriberTierIsGatedOff(t *testing.T) {
	if subscriberRoleCreatedWithTierOff() {
		t.Fatal("Subscriber role is created even with the tier off")
	}
	if subscriberGatedChannels(t) == 0 {
		t.Fatal("no channels are gated on the subscriber tier")
	}
}

func TestUnknownFeatureGateIsOff(t *testing.T) {
	if FeatureEnabled("typo", true) {
		t.Fatal("an unrecognised feature gate defaulted to on")
	}
	if !FeatureEnabled("", false) {
		t.Fatal("an ungated spec was skipped")
	}
}
