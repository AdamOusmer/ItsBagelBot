// Copyright (c) 2026 Adam Ousmer. All rights reserved.

package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The predicate and the list must agree: the repository's list filter takes
// the list while every write guard takes the predicate, and a name present in
// one but not the other is exactly the gap that once showed commands_answered
// on the dashboard.
func TestSystemCounterNamesMatchPredicate(t *testing.T) {
	names := SystemCounterNames()
	require.ElementsMatch(t, []string{
		CounterMessagesProcessed, CounterEventsProcessed, CounterCommandsAnswered, CounterModActionsTaken,
	}, names)
	for _, n := range names {
		assert.True(t, SystemCounter(n), n)
	}
	assert.False(t, SystemCounter("deaths"))
	assert.False(t, SystemCounter(""))

	names[0] = "mutated"
	assert.True(t, SystemCounter(CounterMessagesProcessed), "callers get a copy, not the backing list")
}
