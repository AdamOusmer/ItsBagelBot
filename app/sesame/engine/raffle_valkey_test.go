// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The raffle store's Valkey paths need a live backend; these tests pin the
// logic around them: the receipt digest, the winner pick, and the expiry
// watcher's key filtering (which must reject foreign keys before any client
// call — asserted here with a nil client).

func TestDigestPoolGolden(t *testing.T) {
	// Golden value: the digest is a receipt — it must never drift. If this
	// changes deliberately, bump the version tag inside DigestPool so old
	// receipts verify under the scheme they were written with.
	assert.Equal(t,
		"5e80aac76d88081d8d97f4ff129a72f9bc3191088d05d6d1070b81fbe434f957",
		DigestPool([]string{"alice", "bob", "zoe"}))
}

func TestDigestPoolBindsToTheExactPool(t *testing.T) {
	base := []string{"alice", "bob", "zoe"}
	d := DigestPool(base)

	// Same pool, same digest.
	assert.Equal(t, d, DigestPool([]string{"alice", "bob", "zoe"}))

	// Any change to the pool — order (the canonical form is join-time sorted),
	// membership, or size — must be visible.
	assert.NotEqual(t, d, DigestPool([]string{"bob", "alice", "zoe"}))
	assert.NotEqual(t, d, DigestPool([]string{"alice", "bob", "eve"}))
	assert.NotEqual(t, d, DigestPool([]string{"alice", "bob"}))
}

func TestRngPickDistinctInRange(t *testing.T) {
	for range 200 {
		pick := rngPick(10, 4)
		require.Len(t, pick, 4)
		seen := map[int]bool{}
		for _, idx := range pick {
			assert.GreaterOrEqual(t, idx, 0)
			assert.Less(t, idx, 10)
			assert.False(t, seen[idx], "pick returned a duplicate index")
			seen[idx] = true
		}
	}
}

func TestRngPickSingleCoversWholeRange(t *testing.T) {
	hits := map[int]bool{}
	for range 500 {
		hits[rngPick(3, 1)[0]] = true
	}
	assert.Len(t, hits, 3, "a single-winner draw should reach every entrant over time")
}

// onExpired rides the shared expired-keys firehose; everything that is not a
// well-formed raffle deadline must be dropped before any Valkey call. A nil
// client makes any leak panic, which is the assertion.
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

	out := expandTokens("{targets} won {count}/{entrants}", map[string]string{
		"targets": "@x", "count": "2", "entrants": "9"})
	assert.Equal(t, "@x won 2/9", out)
	assert.Equal(t, "{unknown} stays", expandTokens("{unknown} stays", map[string]string{}))
}
