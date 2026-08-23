// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChatterRosterObserveResolve(t *testing.T) {
	r := newChatterRoster()

	_, ok := r.Resolve(1, "bob")
	assert.False(t, ok, "empty roster resolves nothing")

	r.Observe(1, "Bob", "7", "Bob")
	v, ok := r.Resolve(1, "bob")
	assert.True(t, ok)
	assert.Equal(t, uint64(7), v.ID)
	assert.Equal(t, "bob", v.Login, "lookup keys on the lower-cased login")
	assert.Equal(t, "Bob", v.Name)

	// Channels are isolated.
	_, ok = r.Resolve(2, "bob")
	assert.False(t, ok)

	// A later line refreshes identity fields.
	r.Observe(1, "bob", "7", "Robert")
	v, _ = r.Resolve(1, "bob")
	assert.Equal(t, "Robert", v.Name)

	// An unnamed cohort line must not clobber the learned display name.
	r.Observe(1, "bob", "7", "")
	v, _ = r.Resolve(1, "bob")
	assert.Equal(t, "Robert", v.Name)

	// Garbage identities never enter the roster.
	r.Observe(1, "", "9", "X")     // no login
	r.Observe(1, "x", "", "X")     // no id
	r.Observe(1, "x", "zero", "X") // unparseable id
	r.Observe(0, "x", "9", "X")    // no broadcaster
	for _, login := range []string{"", "x"} {
		_, ok := r.Resolve(1, login)
		assert.False(t, ok)
	}
}

func TestChatterRosterBoundedPerChannel(t *testing.T) {
	r := newChatterRoster()
	for i := range rosterCapacityPerChannel + 10 {
		r.Observe(1, fmt.Sprintf("viewer%d", i), fmt.Sprintf("%d", i+1), "")
	}
	r.mu.RLock()
	size := len(r.chans[1])
	r.mu.RUnlock()
	assert.LessOrEqual(t, size, rosterCapacityPerChannel,
		"the per-channel set stays bounded even if a channel outruns the cap")

	// The bound is per channel, not global.
	r.Observe(2, "viewer0", "1", "")
	r.mu.RLock()
	size2 := len(r.chans[2])
	r.mu.RUnlock()
	assert.Equal(t, 1, size2)
}

func TestChatterRosterNilSafe(t *testing.T) {
	var r *chatterRoster
	assert.NotPanics(t, func() { r.Observe(1, "bob", "7", "Bob") })
	_, ok := r.Resolve(1, "bob")
	assert.False(t, ok)
}
