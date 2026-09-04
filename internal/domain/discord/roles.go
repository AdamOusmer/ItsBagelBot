// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "strings"

// Role slots are the stable keys the dashboard, the setup fill and
// Config.PinnedRoles all name a template role by. They are NOT the role's
// display name: a streamer who renames "Mods" to "Staff" must keep the
// slot, and a slot travelling through JSON must not carry a space.
const (
	SlotOwner      = "owner"
	SlotLeadMod    = "leadMod"
	SlotMods       = "mods"
	SlotVIP        = "vip"
	SlotSubscriber = "subscriber"
	SlotRegulars   = "regulars"
	SlotMember     = "member"
)

// slotByRoleName maps a template role's display name to its slot. Setup
// walks CommunityRoles (which carry names) and needs the slot to look a
// pinned id up; keeping the mapping here means adding a role touches one
// table, not one table per package.
var slotByRoleName = map[string]string{
	RoleOwner:      SlotOwner,
	RoleLeadMod:    SlotLeadMod,
	RoleMods:       SlotMods,
	RoleVIP:        SlotVIP,
	RoleSubscriber: SlotSubscriber,
	RoleRegulars:   SlotRegulars,
	RoleMember:     SlotMember,
}

// RoleSlots is every valid slot key, in template order.
func RoleSlots() []string {
	return []string{SlotOwner, SlotLeadMod, SlotMods, SlotVIP, SlotSubscriber, SlotRegulars, SlotMember}
}

// SlotForRoleName returns the slot a template role name belongs to, or ""
// for a name that is not part of the template.
func SlotForRoleName(name string) string { return slotByRoleName[name] }

// ValidSlot reports whether slot is one of the template slots.
func ValidSlot(slot string) bool {
	for _, s := range slotByRoleName {
		if s == slot {
			return true
		}
	}
	return false
}

// PinnedRoleMap parses PinnedRoles into slot -> role id. Malformed entries
// are dropped rather than failing the whole parse: this runs on the hot
// config path where a zero Config already means "do nothing", and
// ValidateConfig is where a streamer is told about a bad pair.
func (c Config) PinnedRoleMap() map[string]string {
	entries := splitList(c.PinnedRoles)
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		slot, id, ok := splitPin(entry)
		if !ok {
			continue
		}
		out[slot] = id
	}
	return out
}

// splitPin splits one "slot=roleId" pair. Both halves must be non-empty and
// the slot must be a known one.
func splitPin(entry string) (slot, id string, ok bool) {
	slot, id, found := strings.Cut(entry, "=")
	slot = strings.TrimSpace(slot)
	id = strings.TrimSpace(id)
	if !found || slot == "" || id == "" || !ValidSlot(slot) {
		return "", "", false
	}
	return slot, id, true
}

// PinnedRole returns the guild role id the streamer pinned to slot, or ""
// when nothing is pinned there.
func (c Config) PinnedRole(slot string) string { return c.PinnedRoleMap()[slot] }

// IsStaff reports whether a member holding memberRoles counts as staff for
// cfg: Owner, Lead Mod, Mods, or any role on the ticket staff list.
//
// This is the ROLE half of the staff question. The permission half
// (decode.CanMod, which reads Discord's own computed bitfield on an
// interaction) answers a different one: a server admin with no Bagel role
// is a moderator by permission but not staff by role, and a Lead Mod whose
// role lost a permission bit is still staff. Callers that gate a Bagel
// feature want either to be enough, so they check both.
func IsStaff(memberRoles []string, cfg Config) bool {
	if len(memberRoles) == 0 {
		return false
	}
	held := make(map[string]bool, len(memberRoles))
	for _, r := range memberRoles {
		held[r] = true
	}
	for _, id := range cfg.TicketStaffRoleIDs() {
		if held[id] {
			return true
		}
	}
	for _, id := range cfg.StaffRoleIDs() {
		if held[id] {
			return true
		}
	}
	return false
}
