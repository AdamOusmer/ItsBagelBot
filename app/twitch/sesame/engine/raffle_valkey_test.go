// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDigestPoolGolden(t *testing.T) {
	assert.Equal(t,
		"5e80aac76d88081d8d97f4ff129a72f9bc3191088d05d6d1070b81fbe434f957",
		DigestPool([]string{"alice", "bob", "zoe"}))
}

func TestDigestPoolBindsToTheExactPool(t *testing.T) {
	base := []string{"alice", "bob", "zoe"}
	d := DigestPool(base)

	assert.Equal(t, d, DigestPool([]string{"alice", "bob", "zoe"}))

	assert.NotEqual(t, d, DigestPool([]string{"bob", "alice", "zoe"}))
	assert.NotEqual(t, d, DigestPool([]string{"alice", "bob", "eve"}))
	assert.NotEqual(t, d, DigestPool([]string{"alice", "bob"}))
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

func TestOnExpiredIgnoresForeignKeys(t *testing.T) {
	s := &ValkeyRaffleStore{log: zap.NewNop()}
	for _, key := range []string{
		"timer:5:abc",
		"live:5",
		"raffle:snap:5:1700000000000",
		"raffle:claim:5",
		"raffle:rclaim:5",
		"raffle:last:5",
		"raffle:deadline:",
		"raffle:deadline:notanumber",
		"raffle:deadline:0",
		"raffle:remind:notanumber",
		"raffle:remind:0",
		"",
	} {
		s.onExpired(t.Context(), key)
	}
}

func TestMentionListAndTokens(t *testing.T) {
	assert.Equal(t, "@a, @b", mentionList([]string{"a", "b"}))
	assert.Empty(t, mentionList(nil))

	out := expandTokens("", tokenExpansion{
		text: "{targets} won {count}/{entrants}",
		kv:   []string{"targets", "@x", "count", "2", "entrants", "9"},
	})
	assert.Equal(t, "@x won 2/9", out)
	assert.Equal(t, "{unknown} stays", expandTokens("", tokenExpansion{text: "{unknown} stays"}))
}

func TestExpandTokensReadsTheSpanGrammar(t *testing.T) {
	assert.Equal(t, "@x won", expandTokens("", tokenExpansion{text: "{TARGETS} won", kv: []string{"targets", "@x"}}))
	assert.Equal(t, "@x won", expandTokens("", tokenExpansion{text: "{Targets} won", kv: []string{"targets", "@x"}}))
	assert.Equal(t, "nobody won", expandTokens("", tokenExpansion{text: "{targets|nobody} won", kv: []string{"targets", ""}}))
	assert.Equal(t, "{missing|x}", expandTokens("", tokenExpansion{text: "{missing|x}"}))
}
