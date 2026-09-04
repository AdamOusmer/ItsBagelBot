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

// PinnedRoleMap parses PinnedRoles into slot -> role id. A slot pinned twice
// keeps the LAST pair (the map write order), which ValidateConfig reports as
// duplicate_slot rather than silently picking for the streamer. Malformed
// entries are dropped rather than failing the whole parse: this runs on the hot
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

// cutPin splits one "slot=roleId" pair into its trimmed halves. ok is false
// when the entry is not a PAIR at all -- no "=", or a half left empty --
// which is a different mistake from naming a slot that does not exist, and
// the validator reports the two under different codes so the dashboard can
// say "write slot=roleId" rather than "unknown slot" for `owner`.
func cutPin(entry string) (slot, id string, ok bool) {
	slot, id, found := strings.Cut(entry, "=")
	slot = strings.TrimSpace(slot)
	id = strings.TrimSpace(id)
	if !found || slot == "" || id == "" {
		return "", "", false
	}
	return slot, id, true
}

// splitPin splits one "slot=roleId" pair. Both halves must be non-empty and
// the slot must be a known one.
func splitPin(entry string) (slot, id string, ok bool) {
	slot, id, ok = cutPin(entry)
	if !ok || !ValidSlot(slot) {
		return "", "", false
	}
	return slot, id, true
}

// PinnedRole returns the guild role id the streamer pinned to slot, or ""
// when nothing is pinned there.
func (c Config) PinnedRole(slot string) string { return c.PinnedRoleMap()[slot] }

// IsModStaff reports whether a member holding memberRoles is MODERATION
// staff for cfg: Owner, Lead Mod, or Mods.
//
// The ticket desk's staff list is deliberately NOT consulted here. That list
// is a VISIBILITY grant -- a streamer adds "Support" to it so those people
// can read and answer ticket channels -- and the single IsStaff this replaced
// silently turned every such grant into ban/kick/timeout/purge rights on the
// whole guild. A helper role must be addable to the desk without becoming a
// moderator; see IsTicketStaff for the desk half.
//
// This is the ROLE half of the staff question. The permission half
// (decode.CanMod, which reads Discord's own computed bitfield on an
// interaction) answers a different one: a server admin with no Bagel role
// is a moderator by permission but not staff by role, and a Lead Mod whose
// role lost a permission bit is still staff. Callers that gate a Bagel
// feature want either to be enough, so they check both.
func IsModStaff(memberRoles []string, cfg Config) bool {
	return holdsAny(memberRoles, cfg.StaffRoleIDs())
}

// IsTicketStaff reports whether a member may act on the ticket desk: mod
// staff (IsModStaff) plus every role on ticketStaffRoleIds. Strictly wider
// than IsModStaff, and it grants nothing outside the desk.
func IsTicketStaff(memberRoles []string, cfg Config) bool {
	if IsModStaff(memberRoles, cfg) {
		return true
	}
	return holdsAny(memberRoles, cfg.TicketStaffRoleIDs())
}

// holdsAny reports whether memberRoles contains any of want. Empty ids on
// either side never match: an unconfigured guild stores "" in its role
// fields, and matching those would make every member staff.
func holdsAny(memberRoles, want []string) bool {
	if len(memberRoles) == 0 || len(want) == 0 {
		return false
	}
	held := make(map[string]bool, len(memberRoles))
	for _, r := range memberRoles {
		if r != "" {
			held[r] = true
		}
	}
	for _, id := range want {
		if id != "" && held[id] {
			return true
		}
	}
	return false
}
