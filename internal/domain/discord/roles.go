// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"sort"
	"strings"
)

type Slot string

type RoleIDs []string

type heldRoles map[string]bool

type Pin struct {
	Slot Slot
	ID   string
}

const (
	SlotOwner      = "owner"
	SlotLeadMod    = "leadMod"
	SlotMods       = "mods"
	SlotVIP        = "vip"
	SlotSubscriber = "subscriber"
	SlotRegulars   = "regulars"
	SlotMember     = "member"
)

var slotByRoleName = map[string]Slot{
	RoleOwner:      SlotOwner,
	RoleLeadMod:    SlotLeadMod,
	RoleMods:       SlotMods,
	RoleVIP:        SlotVIP,
	RoleSubscriber: SlotSubscriber,
	RoleRegulars:   SlotRegulars,
	RoleMember:     SlotMember,
}

func RoleSlots() []Slot {
	return []Slot{SlotOwner, SlotLeadMod, SlotMods, SlotVIP, SlotSubscriber, SlotRegulars, SlotMember}
}

func SlotForRoleName(name string) Slot { return slotByRoleName[name] }

func ValidSlot(slot Slot) bool {
	for _, s := range slotByRoleName {
		if s == slot {
			return true
		}
	}
	return false
}

func (c Config) PinnedRoleMap() map[string]string {
	entries := splitList(listText(c.PinnedRoles))
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		pin, ok := splitPin(entry)
		if !ok {
			continue
		}
		out[string(pin.Slot)] = pin.ID
	}
	return out
}

func cutPin(entry string) (Pin, bool) {
	slot, id, found := strings.Cut(entry, "=")
	slot = strings.TrimSpace(slot)
	id = strings.TrimSpace(id)
	if !found {
		return Pin{}, false
	}
	if slot == "" || id == "" {
		return Pin{}, false
	}
	return Pin{Slot: Slot(slot), ID: id}, true
}

func splitPin(entry string) (Pin, bool) {
	pin, ok := cutPin(entry)
	if !ok || !ValidSlot(pin.Slot) {
		return Pin{}, false
	}
	return pin, true
}

func (c Config) PinnedRole(slot Slot) string { return c.PinnedRoleMap()[string(slot)] }

func IsModStaff(memberRoles RoleIDs, cfg Config) bool {
	return holdsAny(memberRoles, cfg.StaffRoleIDs())
}

func IsTicketStaff(memberRoles RoleIDs, cfg Config) bool {
	if IsModStaff(memberRoles, cfg) {
		return true
	}
	return holdsAny(memberRoles, cfg.TicketStaffRoleIDs())
}

func holdsAny(memberRoles, want RoleIDs) bool {
	if len(memberRoles) == 0 || len(want) == 0 {
		return false
	}
	return anyHeld(roleSet(memberRoles), want)
}

func roleSet(memberRoles RoleIDs) heldRoles {
	held := make(heldRoles, len(memberRoles))
	for _, r := range memberRoles {
		if r != "" {
			held[r] = true
		}
	}
	return held
}

func anyHeld(held heldRoles, want RoleIDs) bool {
	for _, id := range want {
		if id != "" && held[id] {
			return true
		}
	}
	return false
}

func FormatPinnedRoles(pins map[string]string) string {
	if len(pins) == 0 {
		return ""
	}
	slots := make([]string, 0, len(pins))
	for slot := range pins {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	pairs := make([]string, 0, len(slots))
	for _, slot := range slots {
		pairs = append(pairs, slot+"="+pins[slot])
	}
	return strings.Join(pairs, ",")
}
