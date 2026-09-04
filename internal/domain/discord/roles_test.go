// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"testing"
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
			if len(got) != len(tc.want) {
				t.Fatalf("map = %v, want %v", got, tc.want)
			}
			for slot, id := range tc.want {
				if got[slot] != id {
					t.Fatalf("slot %q = %q, want %q", slot, got[slot], id)
				}
			}
		})
	}
}

func TestPinnedRoleLooksUpOneSlot(t *testing.T) {
	cfg := Config{PinnedRoles: "mods=111,vip=222"}
	if got := cfg.PinnedRole(SlotMods); got != "111" {
		t.Fatalf("mods = %q, want 111", got)
	}
	if got := cfg.PinnedRole(SlotOwner); got != "" {
		t.Fatalf("unpinned owner = %q, want empty", got)
	}
}

// Every template role must map to a slot, or setup silently stops adopting
// pinned ids for it.
func TestEveryTemplateRoleHasASlot(t *testing.T) {
	for _, spec := range CommunityRoles() {
		if SlotForRoleName(spec.Name) == "" {
			t.Fatalf("template role %q has no slot", spec.Name)
		}
	}
	if len(RoleSlots()) != len(CommunityRoles()) {
		t.Fatalf("slots = %d, template roles = %d", len(RoleSlots()), len(CommunityRoles()))
	}
}

func TestIsStaff(t *testing.T) {
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
		// A guild that configured no roles must not grant staff to everyone
		// holding an empty-string role id.
		{"unconfigured guild", Config{}, []string{""}, false},
		{"ticket staff list", withDesk, []string{"helper2"}, true},
		// The desk list REPLACES the fallback, but never drops the roles it
		// explicitly names alongside the owner tier.
		{"owner still staff with a desk list", withDesk, []string{"o"}, true},
		{"lead mod not on desk list", withDesk, []string{"l"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStaff(tc.roles, tc.cfg); got != tc.want {
				t.Fatalf("IsStaff = %v, want %v", got, tc.want)
			}
		})
	}
}
