// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"testing"

	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
)

// rosterChatEnvelope is a minimal chat envelope for the feed-level tests.
var rosterChatEnvelope = lane.Envelope{
	Type:              chatType,
	BroadcasterUserID: "123",
	ChatterUserID:     "7",
	ChatterUserLogin:  "bob",
	ChatterUserName:   "Bob",
}

func TestChatterRosterObserveResolve(t *testing.T) {
	r := newChatterRoster()

	_, ok := r.Resolve(1, "bob")
	assert.False(t, ok, "empty roster resolves nothing")

	r.Observe(1, chatterIdentity{login: "Bob", id: "7", name: "Bob"})
	v, ok := r.Resolve(1, "bob")
	assert.True(t, ok)
	assert.Equal(t, uint64(7), v.ID)
	assert.Equal(t, "bob", v.Login, "lookup keys on the lower-cased login")
	assert.Equal(t, "Bob", v.Name)

	// Channels are isolated.
	_, ok = r.Resolve(2, "bob")
	assert.False(t, ok)

	// A later line refreshes identity fields.
	r.Observe(1, chatterIdentity{login: "bob", id: "7", name: "Robert"})
	v, _ = r.Resolve(1, "bob")
	assert.Equal(t, "Robert", v.Name)

	// An unnamed observation must not clobber the learned display name.
	r.Observe(1, chatterIdentity{login: "bob", id: "7"})
	v, _ = r.Resolve(1, "bob")
	assert.Equal(t, "Robert", v.Name)
}

func TestChatterRosterDropsUnusableIdentities(t *testing.T) {
	r := newChatterRoster()
	r.Observe(1, chatterIdentity{login: "", id: "9", name: "X"})     // no login
	r.Observe(1, chatterIdentity{id: "", login: "x", name: "X"})     // no id
	r.Observe(1, chatterIdentity{id: "zero", login: "x", name: "X"}) // unparseable id
	r.Observe(0, chatterIdentity{id: "9", login: "x", name: "X"})    // no broadcaster
	for _, login := range []string{"", "x"} {
		_, ok := r.Resolve(1, login)
		assert.False(t, ok)
	}
}

func TestChatterRosterBoundedPerChannel(t *testing.T) {
	r := newChatterRoster()
	for i := range rosterCapacityPerChannel + 10 {
		r.Observe(1, chatterIdentity{login: fmt.Sprintf("viewer%d", i), id: fmt.Sprintf("%d", i+1)})
	}
	r.mu.RLock()
	size := len(r.chans[1])
	r.mu.RUnlock()
	assert.LessOrEqual(t, size, rosterCapacityPerChannel,
		"the per-channel set stays bounded even if a channel outruns the cap")

	// The bound is per channel, not global.
	r.Observe(2, chatterIdentity{login: "viewer0", id: "1"})
	r.mu.RLock()
	size2 := len(r.chans[2])
	r.mu.RUnlock()
	assert.Equal(t, 1, size2)
}

func TestChatterRosterNilSafe(t *testing.T) {
	var r *chatterRoster
	assert.NotPanics(t, func() {
		r.ObserveEnvelope(1, &rosterChatEnvelope)
	})
	_, ok := r.Resolve(1, "bob")
	assert.False(t, ok)
}
