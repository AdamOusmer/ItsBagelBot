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

// Viewer tiers an autorole plan can be built for. Empty means "no linked
// Twitch identity", which is not the same as "linked and plain": an unknown
// viewer must never have tier roles taken away (see AutoRolePlan).
const (
	TierNone       = ""
	TierSubscriber = "subscriber"
	TierVIP        = "vip"
	TierRegular    = "regular"
)

// RolePlan is the role changes to apply to one member.
type RolePlan struct {
	Add    []string
	Remove []string
}

// Empty reports whether the plan asks for nothing.
func (p RolePlan) Empty() bool { return len(p.Add) == 0 && len(p.Remove) == 0 }

// AutoRolePlan computes the role changes for one member given their linked
// Twitch tier and the roles they hold now.
//
// Two rules make this safe to run on every join:
//
//   - A member role is only ADDED when missing, never removed, so a
//     manually granted role is never taken back.
//   - Tier roles are removed only when tier is a KNOWN one. TierNone means
//     the viewer has no linked identity at all, and stripping a VIP role
//     from someone whose link merely has not been read yet is the one
//     failure a streamer would notice and could not undo.
//
// join controls the member role: it is granted on join and on an explicit
// (re)link, not on every tier recomputation.
func AutoRolePlan(cfg Config, tier string, current []string, join bool) RolePlan {
	held := make(map[string]bool, len(current))
	for _, r := range current {
		held[r] = true
	}
	var plan RolePlan
	if join && cfg.MemberRoleID != "" && !held[cfg.MemberRoleID] {
		plan.Add = append(plan.Add, cfg.MemberRoleID)
	}
	if tier == TierNone {
		return plan
	}
	want := cfg.tierRoleID(tier)
	if want != "" && !held[want] {
		plan.Add = append(plan.Add, want)
	}
	for _, id := range cfg.tierRoleIDs() {
		if id != want && held[id] {
			plan.Remove = append(plan.Remove, id)
		}
	}
	return plan
}

// tierRoleID maps a viewer tier onto the guild role that represents it.
func (c Config) tierRoleID(tier string) string {
	switch tier {
	case TierSubscriber:
		return c.SubscriberRoleID
	case TierVIP:
		return c.VIPRoleID
	case TierRegular:
		return c.RegularsRoleID
	default:
		return ""
	}
}

// tierRoleIDs is every role autorole owns, and therefore every role it may
// take away. Roles outside this set (staff, member) are never revoked.
func (c Config) tierRoleIDs() []string {
	out := make([]string, 0, 3)
	for _, id := range []string{c.SubscriberRoleID, c.VIPRoleID, c.RegularsRoleID} {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}
