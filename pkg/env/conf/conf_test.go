// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package conf

import "testing"

// TestProjectionDefaults pins the subjects each loader answers with when
// nothing is set, which is the deployed state: no manifest sets any of these.
// The two loaders must differ in exactly the modules and commands subjects,
// and the internal projection subjects must be the same bytes in both — that
// equality is the bug this package was created to make unrepeatable.
func TestProjectionDefaults(t *testing.T) {
	internal, viaProjector := LoadProjection(), LoadProjectionViaProjector()

	got := []string{
		internal.ProjectionUsersSubject,
		internal.ProjectionModulesSubject,
		internal.ProjectionCommandsSubject,
		internal.CacheInvalidationPrefix,
		viaProjector.ProjectionUsersSubject,
		viaProjector.ProjectionModulesSubject,
		viaProjector.ProjectionCommandsSubject,
		viaProjector.CacheInvalidationPrefix,
	}
	want := []string{
		"bagel.rpc.internal.projection.users.get",
		"bagel.rpc.internal.projection.modules.get",
		"bagel.rpc.internal.projection.commands.get",
		"bagel.cache.invalidate",
		"bagel.rpc.internal.projection.users.get",
		"bagel.rpc.projector.dashboard.modules.get",
		"bagel.rpc.projector.dashboard.commands.get",
		"bagel.cache.invalidate",
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("subject %d = %q, want %q", i, got[i], w)
		}
	}
}

// TestLaneDefaults pins the lane subjects both pipeline services bind.
func TestLaneDefaults(t *testing.T) {
	l := LoadLanes()
	got := []string{l.PremiumSubject, l.StandardSubject, l.OutgressPremiumSubject, l.OutgressStandardSubject}
	want := []string{
		"twitch.ingress.event.premium",
		"twitch.ingress.event.standard",
		"twitch.outgress.premium",
		"twitch.outgress.standard",
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("lane %d = %q, want %q", i, got[i], w)
		}
	}
}
