// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import "ItsBagelBot/internal/moderation"

// The infrastructure floor lists live in internal/moderation (shared with the
// save-time validators); this file only maps them onto chat verdicts. The term
// lists themselves are NOT mirrored here: moderation.MatchFloor owns the
// matching (word-bounded, allocation-free) and hands back a FloorKind, so this
// file carries only the kind -> verdict mapping.

// category is one named blocklist plus the action a match implies.
type category struct {
	name    string
	action  Action
	seconds uint32
}

// defaultCategories returns the floor verdict table INDEXED BY
// moderation.FloorKind, not a list to be searched. MatchFloor already returns
// the enum, so the lookup is one bounds-checked index instead of a linear scan
// over names comparing against kind.String() (which the old shape called per
// candidate). A slice rather than a fixed-size array on purpose: moderation
// exports no "number of kinds" constant, and floorInfra's length check then
// degrades to "no verdict" for an unmapped kind exactly like the old scan did,
// instead of panicking on the hot path if a FloorKind is ever added upstream.
// Index 0 is FloorNone and is deliberately the zero category (empty name).
func defaultCategories() []category {
	cats := make([]category, moderation.FloorScam+1)
	cats[moderation.FloorIPLogger] = category{name: "ip_logger", action: ActionTimeout, seconds: 600}
	cats[moderation.FloorScam] = category{name: "scam", action: ActionTimeout, seconds: 600}
	return cats
}
