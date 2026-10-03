// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedRoleMapParses(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{"empty", "", nil},
		{"one pair", "mods=111", map[string]string{SlotMods: "111"}},
		{"spaces trimmed", " owner = 222 , member=333 ", map[string]string{SlotOwner: "222", SlotMember: "333"}},
		{"camelCase slot survives", "leadMod=444", map[string]string{SlotLeadMod: "444"}},
		{"unknown slot dropped", "janitor=555", map[string]string{}},
		{"missing id dropped", "mods=", map[string]string{}},
		{"missing separator dropped", "mods", map[string]string{}},
		{"last pair for a slot wins", "mods=1,mods=2", map[string]string{SlotMods: "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Config{PinnedRoles: tc.raw}.PinnedRoleMap()
			assert.Len(t, got, len(tc.want))
			for slot, id := range tc.want {
				assert.Equal(t, id, got[slot], slot)
			}
		})
	}
}

func TestPinnedRoleLooksUpOneSlot(t *testing.T) {
	cfg := Config{PinnedRoles: "mods=111,vip=222"}

	assert.Equal(t, "111", cfg.PinnedRole(SlotMods))
	assert.Empty(t, cfg.PinnedRole(SlotOwner), "an unpinned slot reads empty")
}

func TestEveryTemplateRoleHasASlot(t *testing.T) {
	for _, spec := range CommunityRoles() {
		assert.NotEmpty(t, SlotForRoleName(spec.Name), "template role %q has no slot", spec.Name)
	}
	assert.Len(t, RoleSlots(), len(CommunityRoles()))
}

func TestIsModStaff(t *testing.T) {
	cfg := Config{OwnerRoleID: "o", LeadModRoleID: "l", ModsRoleID: "m"}
	withDesk := Config{OwnerRoleID: "o", TicketStaffRoles: "helper1,helper2"}
	cases := []struct {
		name  string
		cfg   Config
		roles []string
		want  bool
	}{
		{"no roles", cfg, nil, false},
		{"owner", cfg, []string{"o"}, true},
		{"lead mod", cfg, []string{"x", "l"}, true},
		{"mods", cfg, []string{"m"}, true},
		{"stranger", cfg, []string{"x"}, false},
		{"unconfigured guild", Config{}, []string{""}, false},
		{"desk helper is not mod staff", withDesk, []string{"helper2"}, false},
		{"owner still mod staff with a desk list", withDesk, []string{"o"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsModStaff(tc.roles, tc.cfg); got != tc.want {
				t.Fatalf("IsModStaff = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsTicketStaff(t *testing.T) {
	cfg := Config{OwnerRoleID: "o", LeadModRoleID: "l", ModsRoleID: "m"}
	withDesk := Config{OwnerRoleID: "o", TicketStaffRoles: "helper1,helper2"}
	cases := []struct {
		name  string
		cfg   Config
		roles []string
		want  bool
	}{
		{"no roles", cfg, nil, false},
		{"stranger", cfg, []string{"x"}, false},
		{"mods without a desk list", cfg, []string{"m"}, true},
		{"desk helper", withDesk, []string{"helper2"}, true},
		{"owner still desk staff", withDesk, []string{"o"}, true},
		{"lead mod not on desk list", withDesk, []string{"l"}, false},
		{"unconfigured guild", Config{}, []string{""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTicketStaff(tc.roles, tc.cfg); got != tc.want {
				t.Fatalf("IsTicketStaff = %v, want %v", got, tc.want)
			}
		})
	}
	if !IsTicketStaff([]string{"o"}, withDesk) || !IsModStaff([]string{"o"}, withDesk) {
		t.Fatal("mod staff must always be ticket staff")
	}
}

func TestFormatPinnedRoles(t *testing.T) {
	pins := map[string]string{SlotMods: "100000000000000001", SlotOwner: "100000000000000002"}

	raw := FormatPinnedRoles(pins)

	assert.Equal(t, "mods=100000000000000001,owner=100000000000000002", raw, "slots are sorted")
	assert.Equal(t, pins, Config{PinnedRoles: raw}.PinnedRoleMap(), "the format round trips")
	assert.Empty(t, FormatPinnedRoles(nil), "no pins format as the empty (unset) field")

	bad := ValidateConfig(Config{PinnedRoles: FormatPinnedRoles(map[string]string{SlotMods: ""})})
	require.Len(t, bad, 1, "an empty id stays visible to the validator")
	assert.Equal(t, CodeMalformedPair, bad[0].Code)
}
