// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func roleNamed(t *testing.T, name string) RoleSpec {
	t.Helper()
	for _, r := range CommunityRoles() {
		if r.Name == name {
			return r
		}
	}
	require.FailNow(t, "role missing from the template", name)
	return RoleSpec{}
}

func TestBotPermissionPolicy(t *testing.T) {
	assert.Zero(t, BotPermissions&int(PermAdministrator), "the bot invite must not request Administrator")
	for name, bit := range map[string]int64{
		"kick": PermKickMembers, "ban": PermBanMembers, "manage messages": PermManageMessages, "view audit log": PermViewAuditLog, "timeout (MODERATE_MEMBERS)": PermModerateMembers,
	} {
		assert.NotZero(t, int64(BotPermissions)&bit, "the bot must request %s", name)
	}
}

func roleNames(roles []RoleSpec) []string {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = r.Name
	}
	return names
}

func roleColours(roles []RoleSpec) map[int][]string {
	colours := map[int][]string{}
	for _, r := range roles {
		if r.Color != 0 {
			colours[r.Color] = append(colours[r.Color], r.Name)
		}
	}
	return colours
}

func TestCommunityRoleHierarchyAndColours(t *testing.T) {
	roles := CommunityRoles()

	assert.Equal(t, []string{"Owner", "Lead Mod", "Mods", "VIP", "Subscriber", "Regulars", "Member"}, roleNames(roles), "order is the hierarchy")
	colours := roleColours(roles)
	assert.Len(t, colours, 6, "Member is deliberately uncoloured")
	for colour, shared := range colours {
		assert.Len(t, shared, 1, "roles %v share colour %#06x", shared, colour)
	}
	assert.Zero(t, roleNamed(t, RoleMember).Color)
}

func TestCommunityRolePermissions(t *testing.T) {
	assert.NotZero(t, roleNamed(t, RoleLeadMod).Permissions&PermAdministrator, "Lead Mod holds Administrator")
	mods := roleNamed(t, RoleMods).Permissions
	assert.Zero(t, mods&PermAdministrator, "Mods holding Administrator would erase the Lead Mod tier")
	for _, want := range []int64{PermBanMembers, PermKickMembers, PermManageMessages, PermModerateMembers} {
		assert.NotZero(t, mods&want, "Mods is missing permission bit %d", want)
	}
	for _, r := range CommunityRoles() {
		assert.Equal(t, r.Name == RoleLeadMod || r.Name == RoleMods, r.Permissions != 0, "%q: access tiers hold no permissions", r.Name)
	}
}

func gatedChannels() []ChannelSpec {
	return slices.DeleteFunc(CommunityChannels(), func(ch ChannelSpec) bool { return len(ch.AllowRoles) == 0 })
}

func TestCommunityChannelsBindTheRequiredSurfaces(t *testing.T) {
	binds := map[string]ChannelSpec{}
	for _, ch := range CommunityChannels() {
		binds[ch.Bind] = ch
	}

	for _, want := range []string{"live", "clips", "welcome", "voice", "logs", "tickets", "ticketcat", "voicecat"} {
		assert.Contains(t, binds, want, "template missing bind %q", want)
	}
	assert.Equal(t, ChannelVoice, binds["voice"].Type, "the voice bind is the voice hub")
}

func TestGatedChannelsNameExistingRolesAndAdmitStaff(t *testing.T) {
	created := roleNames(CommunityRoles())

	for _, ch := range gatedChannels() {
		for _, role := range ch.AllowRoles {
			assert.Contains(t, created, role, "channel %q gates on a role the template never creates", ch.Name)
		}
		for _, staff := range StaffRoles {
			assert.Contains(t, ch.AllowRoles, staff, "gated channel %q excludes staff role %q", ch.Name, staff)
		}
	}
}

func TestTierAreasAreOrderedByRank(t *testing.T) {
	assert.NotContains(t, VIPRoles, RoleSubscriber, "the VIP area must not admit every subscriber")
	assert.Contains(t, SubscriberRoles, RoleVIP, "VIPs rank above subscribers")
}

func TestSubscriberTierIsGatedOff(t *testing.T) {
	assert.False(t, FeatureEnabled(roleNamed(t, RoleSubscriber).Feature, false), "the Subscriber role is not created with the tier off")
	var gated int
	for _, ch := range CommunityChannels() {
		if ch.Feature != FeatureSubscribers {
			continue
		}
		gated++
		assert.False(t, FeatureEnabled(ch.Feature, false), "channel %q is created with the subscriber tier off", ch.Name)
		assert.True(t, FeatureEnabled(ch.Feature, true), "channel %q is never created with the tier on", ch.Name)
	}
	assert.NotZero(t, gated, "channels are gated on the subscriber tier")
	assert.False(t, FeatureEnabled("typo", true), "an unrecognised feature gate is off")
	assert.True(t, FeatureEnabled("", false), "an ungated spec is always created")
}
