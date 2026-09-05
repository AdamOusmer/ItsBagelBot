// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"maps"
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
		// A guild that configured no roles must not grant staff to everyone
		// holding an empty-string role id.
		{"unconfigured guild", Config{}, []string{""}, false},
		// The privilege split this test exists for: a desk helper is NOT a
		// moderator. Before the split they held ban/kick/timeout/purge.
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
		// No desk list configured: TicketStaffRoleIDs falls back to the
		// Owner/Lead Mod/Mods trio, so mod staff run the desk by default.
		{"mods without a desk list", cfg, []string{"m"}, true},
		{"desk helper", withDesk, []string{"helper2"}, true},
		{"owner still desk staff", withDesk, []string{"o"}, true},
		// The desk list REPLACES the fallback: a Lead Mod the streamer left
		// off the list does not run the desk, but stays mod staff.
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
	// The two are ordered, never independent: mod staff is always desk staff.
	if !IsTicketStaff([]string{"o"}, withDesk) || !IsModStaff([]string{"o"}, withDesk) {
		t.Fatal("mod staff must always be ticket staff")
	}
}

// The map shape and the stored string shape must mean the same thing, or
// the setup RPC validates something other than what gets saved.
func TestFormatPinnedRolesRoundTrips(t *testing.T) {
	pins := map[string]string{
		SlotMods: "100000000000000001", SlotOwner: "100000000000000002",
	}

	raw := FormatPinnedRoles(pins)

	if raw != "mods=100000000000000001,owner=100000000000000002" {
		t.Fatalf("formatted = %q, want the slots sorted", raw)
	}
	back := Config{PinnedRoles: raw}.PinnedRoleMap()
	if !maps.Equal(back, pins) {
		t.Fatalf("round trip = %v, want %v", back, pins)
	}
	if FormatPinnedRoles(nil) != "" {
		t.Fatal("no pins must format as the empty (unset) field")
	}
}

// An empty id is the dashboard clearing a pin without dropping the key. It
// has to reach the validator as the malformed pair it is, not vanish.
func TestFormatPinnedRolesKeepsAnEmptyIDVisibleToTheValidator(t *testing.T) {
	raw := FormatPinnedRoles(map[string]string{SlotMods: ""})

	bad := ValidateConfig(Config{PinnedRoles: raw})
	if len(bad) != 1 || bad[0].Code != CodeMalformedPair {
		t.Fatalf("errors = %+v, want one %s", bad, CodeMalformedPair)
	}
}
