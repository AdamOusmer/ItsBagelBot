// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package conf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultSubjectsAreStable(t *testing.T) {
	internal, viaProjector, lanes := LoadProjection(), LoadProjectionViaProjector(), LoadLanes()

	got := map[string]string{
		"internal users":         internal.ProjectionUsersSubject,
		"internal modules":       internal.ProjectionModulesSubject,
		"internal commands":      internal.ProjectionCommandsSubject,
		"internal invalidation":  internal.CacheInvalidationPrefix,
		"projector users":        viaProjector.ProjectionUsersSubject,
		"projector modules":      viaProjector.ProjectionModulesSubject,
		"projector commands":     viaProjector.ProjectionCommandsSubject,
		"projector invalidation": viaProjector.CacheInvalidationPrefix,
		"ingress premium lane":   lanes.PremiumSubject,
		"ingress standard lane":  lanes.StandardSubject,
		"outgress premium lane":  lanes.OutgressPremiumSubject,
		"outgress standard lane": lanes.OutgressStandardSubject,
	}
	want := map[string]string{
		"internal users":         "bagel.rpc.internal.projection.users.get",
		"internal modules":       "bagel.rpc.internal.projection.modules.get",
		"internal commands":      "bagel.rpc.internal.projection.commands.get",
		"internal invalidation":  "bagel.cache.invalidate",
		"projector users":        "bagel.rpc.internal.projection.users.get",
		"projector modules":      "bagel.rpc.projector.dashboard.modules.get",
		"projector commands":     "bagel.rpc.projector.dashboard.commands.get",
		"projector invalidation": "bagel.cache.invalidate",
		"ingress premium lane":   "twitch.ingress.event.premium",
		"ingress standard lane":  "twitch.ingress.event.standard",
		"outgress premium lane":  "twitch.outgress.premium",
		"outgress standard lane": "twitch.outgress.standard",
	}
	assert.Equal(t, want, got)
}
