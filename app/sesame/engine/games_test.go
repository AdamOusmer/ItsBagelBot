// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- gamble settings ---

func TestClampGambleSettings(t *testing.T) {
	s := ClampGambleSettings(0, 0, 0, 0)
	assert.Equal(t, GambleSettings{
		MinBet: 1, MaxBet: 1000, WinPercent: 50, CooldownSeconds: 10,
	}, s, "zero config falls back to the defaults")

	s = ClampGambleSettings(5, 100, 150, 99999)
	assert.Equal(t, int64(100), s.WinPercent, "chance clamps to the ceiling")
	assert.Equal(t, int64(600), s.CooldownSeconds, "cooldown clamps to the ceiling")
	assert.Equal(t, int64(5), s.MinBet)
	assert.Equal(t, int64(100), s.MaxBet)

	s = ClampGambleSettings(500, 200, -3, 0)
	assert.Equal(t, int64(500), s.MinBet, "a high min raises the max with it")
	assert.Equal(t, int64(500), s.MaxBet)
	assert.Equal(t, int64(50), s.WinPercent, "non-positive chance means unset")
}

// --- bet resolution ---

func TestResolveGambleBet(t *testing.T) {
	const bal = int64(1234)

	t.Run("numbers", func(t *testing.T) {
		bet, out := ResolveGambleBet("100", bal, 1, 1000)
		assert.Equal(t, BetOK, out)
		assert.Equal(t, int64(100), bet)

		_, out = ResolveGambleBet("", bal, 1, 1000)
		assert.Equal(t, BetEmpty, out)

		_, out = ResolveGambleBet("lots", bal, 1, 1000)
		assert.Equal(t, BetInvalid, out)

		_, out = ResolveGambleBet("0", bal, 1, 1000)
		assert.Equal(t, BetInvalid, out)

		_, out = ResolveGambleBet("-5", bal, 1, 1000)
		assert.Equal(t, BetInvalid, out)
	})

	t.Run("limits", func(t *testing.T) {
		_, out := ResolveGambleBet("5", bal, 10, 1000)
		assert.Equal(t, BetBelowMin, out)

		_, out = ResolveGambleBet("2000", bal, 1, 1000)
		assert.Equal(t, BetAboveMax, out)

		_, out = ResolveGambleBet("2000", bal, 1, 5000)
		assert.Equal(t, BetOverBalance, out, "within the cap but over the standing")
	})

	t.Run("derived stakes", func(t *testing.T) {
		bet, out := ResolveGambleBet("all", bal, 1, 1000)
		assert.Equal(t, BetOK, out)
		assert.Equal(t, int64(1000), bet, "all caps at the house max instead of refusing")

		bet, out = ResolveGambleBet("half", bal, 1, 1000)
		assert.Equal(t, BetOK, out)
		assert.Equal(t, int64(617), bet)

		bet, out = ResolveGambleBet("all", 40, 1, 1000)
		assert.Equal(t, BetOK, out)
		assert.Equal(t, int64(40), bet)

		_, out = ResolveGambleBet("all", 0, 1, 1000)
		assert.Equal(t, BetBelowMin, out, "nothing to stake")
	})
}

func TestGambleWinsBoundaries(t *testing.T) {
	assert.True(t, GambleWins(50, 50), "the boundary roll wins")
	assert.False(t, GambleWins(51, 50))
	assert.True(t, GambleWins(1, 1), "one-in-a-hundred still has its one")
	assert.False(t, GambleWins(2, 1))
	assert.True(t, GambleWins(100, 100), "always-win pays always")
}

// --- duel mechanics ---

func TestClampDuelSeconds(t *testing.T) {
	assert.Equal(t, int64(60), ClampDuelSeconds(0, DuelDefaultPotSeconds), "unset takes the default")
	assert.Equal(t, int64(10), ClampDuelSeconds(2, DuelDefaultPotSeconds), "floor")
	assert.Equal(t, int64(1800), ClampDuelSeconds(99999, DuelDefaultPotSeconds), "ceiling")
	assert.Equal(t, int64(45), ClampDuelSeconds(45, DuelDefaultPotSeconds))
}

func TestSortAndPickDuelWinner(t *testing.T) {
	stakes := SortDuelStakes([]DuelStake{
		{Login: "zoe", Stake: 10},
		{Login: "alice", Stake: 30},
		{Login: "bob", Stake: 20},
	})
	require.True(t, stakes[0].Login == "alice" && stakes[1].Login == "bob" && stakes[2].Login == "zoe",
		"canonical order is login-sorted regardless of input order")

	// alice 30 | bob 20 | zoe 10 — cumulative [0,30) [30,50) [50,60)
	assert.Equal(t, "alice", PickDuelWinner(stakes, 0))
	assert.Equal(t, "alice", PickDuelWinner(stakes, 29))
	assert.Equal(t, "bob", PickDuelWinner(stakes, 30), "the boundary lands on the next stake")
	assert.Equal(t, "zoe", PickDuelWinner(stakes, 59))
	assert.Equal(t, "zoe", PickDuelWinner(stakes, 60), "out-of-range rolls fall off the end, never panic")
	assert.Equal(t, "", PickDuelWinner(nil, 0), "an empty pool picks nobody")
}

func TestDigestDuelPoolStable(t *testing.T) {
	a := SortDuelStakes([]DuelStake{{Login: "bob", Stake: 20}, {Login: "alice", Stake: 30}})
	b := SortDuelStakes([]DuelStake{{Login: "alice", Stake: 30}, {Login: "bob", Stake: 20}})
	assert.Equal(t, DigestDuelPool(a), DigestDuelPool(b), "digest binds to the pool, not the iteration order")

	c := SortDuelStakes([]DuelStake{{Login: "alice", Stake: 31}, {Login: "bob", Stake: 20}})
	assert.NotEqual(t, DigestDuelPool(a), DigestDuelPool(c), "a changed stake changes the digest")
}

func TestParseDuelLedger(t *testing.T) {
	entries := SortDuelStakes(parseDuelLedger(map[string]string{
		"opener": "100",
		"bob":    "20",
		"ghost":  "0",      // zero stake: not a real entry
		"junk":   "points", // unreadable: dropped rather than poisoning the pot
	}))
	require.Len(t, entries, 2)
	assert.Equal(t, DuelStake{Login: "bob", Stake: 20}, entries[0])
	assert.Equal(t, DuelStake{Login: "opener", Stake: 100}, entries[1])
	assert.Empty(t, parseDuelLedger(nil))
}
