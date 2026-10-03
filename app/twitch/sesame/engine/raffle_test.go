// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDigestPoolGolden(t *testing.T) {
	assert.Equal(t,
		"5e80aac76d88081d8d97f4ff129a72f9bc3191088d05d6d1070b81fbe434f957",
		DigestPool([]string{"alice", "bob", "zoe"}))
}

func TestDigestPoolBindsToTheExactPool(t *testing.T) {
	d := DigestPool([]string{"alice", "bob", "zoe"})
	cases := []struct {
		name string
		pool []string
		same bool
	}{
		{"the same pool digests the same", []string{"alice", "bob", "zoe"}, true},
		{"a reordered pool differs", []string{"bob", "alice", "zoe"}, false},
		{"a swapped entrant differs", []string{"alice", "bob", "eve"}, false},
		{"a shorter pool differs", []string{"alice", "bob"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.same, DigestPool(tc.pool) == d)
		})
	}
}

func TestPickWinnersDistinctInRange(t *testing.T) {
	pool := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	for range 200 {
		pick := pickWinners(pool, 4)
		require.Len(t, pick, 4)
		seen := map[string]bool{}
		for _, w := range pick {
			assert.False(t, seen[w], "pick returned a duplicate winner")
			seen[w] = true
		}
	}
}

func TestPickWinnersSingleCoversWholePool(t *testing.T) {
	pool := []string{"a", "b", "c"}
	hits := map[string]bool{}
	for range 500 {
		hits[pickWinners(pool, 1)[0]] = true
	}
	assert.Len(t, hits, 3, "a single-winner draw should reach every entrant over time")
}

func TestPickWinnersOversizedAskClampsToCeiling(t *testing.T) {
	pool := []string{"a", "b", "c", "d", "e"}
	pick := pickWinners(pool, 1<<40)
	assert.ElementsMatch(t, pool, pick)
	assert.Empty(t, pickWinners(pool, -5))
}
