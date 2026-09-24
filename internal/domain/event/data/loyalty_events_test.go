// Copyright (c) 2026 Adam Ousmer. All rights reserved.

package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
