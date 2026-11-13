// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/lifecycle"
)

func (r *Users) recordLifecycle(m lifecycleMutation) {
	if m.request.Action != "grant_missing" {
		return
	}
	before := LegacyLifecycle(m.row, m.now)
	if before.GrantHealth != lifecycle.GrantHealthy {
		return
	}
	if m.state.GrantHealth == lifecycle.GrantMissing {
		r.analytics.Count(catalog.Lifecycle, "revoked")
	}
}
