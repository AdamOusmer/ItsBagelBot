// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/lifecycle"
)

type lifecycleEdge struct {
	event   string
	crossed func(before, after lifecycle.State) bool
}

var lifecycleEdges = []lifecycleEdge{
	{event: "revoked", crossed: func(before, after lifecycle.State) bool {
		return before.GrantHealth == lifecycle.GrantHealthy && after.GrantHealth == lifecycle.GrantMissing
	}},
	{event: "chat_banned", crossed: func(before, after lifecycle.State) bool {
		return before.ChatRestriction != lifecycle.RestrictionBanned && after.ChatRestriction == lifecycle.RestrictionBanned
	}},
	{event: "dormant", crossed: func(before, after lifecycle.State) bool {
		return before.RuntimeState != lifecycle.Dormant && after.RuntimeState == lifecycle.Dormant
	}},
	{event: "access_restored", crossed: func(before, after lifecycle.State) bool {
		return before.GrantHealth == lifecycle.GrantMissing && after.GrantHealth == lifecycle.GrantHealthy
	}},
	{event: "unbanned", crossed: func(before, after lifecycle.State) bool {
		return before.ChatRestriction == lifecycle.RestrictionBanned && after.ChatRestriction != lifecycle.RestrictionBanned
	}},
	{event: "woke", crossed: func(before, after lifecycle.State) bool {
		return before.RuntimeState == lifecycle.Dormant && after.RuntimeState != lifecycle.Dormant
	}},
}

var migrationActions = map[string]bool{"seed": true, "seed_ack": true, "seed_blocked": true}

func (r *Users) recordLifecycle(m lifecycleMutation) {
	r.recordDesiredEnabled(m)
	if migrationActions[m.request.Action] {
		return
	}
	before := LegacyLifecycle(m.row, m.now)
	for _, edge := range lifecycleEdges {
		if edge.crossed(before, m.state) {
			r.analytics.Count(catalog.Lifecycle, edge.event)
		}
	}
}

func (r *Users) recordDesiredEnabled(m lifecycleMutation) {
	before := LegacyLifecycle(m.row, m.now)
	if before.DesiredEnabled == m.state.DesiredEnabled {
		return
	}
	event := "bot_disabled"
	if m.state.DesiredEnabled {
		event = "bot_enabled"
	}
	r.analytics.Count(catalog.Lifecycle, event)
}
