package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRaidCooldownTripsOncePerWindow(t *testing.T) {
	rc := newRaidCooldown(time.Minute)
	t0 := time.Unix(1_700_000_000, 0)

	assert.True(t, rc.trip(1, t0), "first trip fires")
	assert.False(t, rc.trip(1, t0.Add(30*time.Second)), "within the window is suppressed")
	assert.True(t, rc.trip(1, t0.Add(90*time.Second)), "after the window fires again")
}

func TestRaidCooldownIsPerChannel(t *testing.T) {
	rc := newRaidCooldown(time.Minute)
	t0 := time.Unix(1_700_000_000, 0)

	assert.True(t, rc.trip(1, t0))
	assert.True(t, rc.trip(2, t0), "a different channel is independent")
	assert.False(t, rc.trip(1, t0.Add(time.Second)))
}

func TestRaidCooldownPrunesStaleEntries(t *testing.T) {
	rc := newRaidCooldown(time.Minute)
	base := time.Unix(1_700_000_000, 0)

	// Fill past the prune threshold with entries that are all stale by `now`.
	for i := 0; i < raidCooldownPruneAbove+1; i++ {
		rc.trip(uint64(i), base)
	}
	// A trip far in the future prunes the stale entries it sweeps.
	rc.trip(9_999_999, base.Add(time.Hour))
	assert.LessOrEqual(t, len(rc.last), 2, "stale entries are swept once the map grows past the bound")
}
