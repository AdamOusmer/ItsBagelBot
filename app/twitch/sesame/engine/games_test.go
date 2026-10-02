// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClampGambleSettings(t *testing.T) {
	cases := []struct {
		name                             string
		minBet, maxBet, chance, cooldown int64
		want                             GambleSettings
	}{
		{
			name: "zero config falls back to the defaults",
			want: GambleSettings{MinBet: 1, MaxBet: 1000, WinPercent: 50, CooldownSeconds: 10},
		},
		{
			name:   "chance and cooldown clamp to their ceilings",
			minBet: 5, maxBet: 100, chance: 150, cooldown: 99999,
			want: GambleSettings{MinBet: 5, MaxBet: 100, WinPercent: 100, CooldownSeconds: 600},
		},
		{
			name:   "a high min raises the max with it and a non-positive chance means unset",
			minBet: 500, maxBet: 200, chance: -3,
			want: GambleSettings{MinBet: 500, MaxBet: 500, WinPercent: 50, CooldownSeconds: 10},
		},
		{
			name:   "configured limits are honored as-is",
			minBet: 5000, maxBet: 10000,
			want: GambleSettings{MinBet: 5000, MaxBet: 10000, WinPercent: 50, CooldownSeconds: 10},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ClampGambleSettings(tc.minBet, tc.maxBet, tc.chance, tc.cooldown))
		})
	}
}

func TestResolveGambleBet(t *testing.T) {
	cases := []struct {
		name           string
		arg            string
		balance        int64
		minBet, maxBet int64
		wantBet        int64
		wantOutcome    GambleBetOutcome
	}{
		{"a number is the stake", "100", 1234, 1, 1000, 100, BetOK},
		{"no argument is empty", "", 1234, 1, 1000, 0, BetEmpty},
		{"words are invalid", "lots", 1234, 1, 1000, 0, BetInvalid},
		{"zero is invalid", "0", 1234, 1, 1000, 0, BetInvalid},
		{"a negative number is invalid", "-5", 1234, 1, 1000, 0, BetInvalid},
		{"below the minimum is refused", "5", 1234, 10, 1000, 0, BetBelowMin},
		{"above the house maximum is refused", "2000", 1234, 1, 1000, 0, BetAboveMax},
		{"within the cap but over the standing is refused", "2000", 1234, 1, 5000, 0, BetOverBalance},
		{"all caps at the house maximum instead of refusing", "all", 1234, 1, 1000, 1000, BetOK},
		{"half stakes half the balance rounded up", "half", 1234, 1, 1000, 617, BetOK},
		{"all stakes a small balance in full", "all", 40, 1, 1000, 40, BetOK},
		{"all with nothing to stake is below the minimum", "all", 0, 1, 1000, 0, BetBelowMin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bet, outcome := ResolveGambleBet(tc.arg, tc.balance, tc.minBet, tc.maxBet)

			assert.Equal(t, tc.wantOutcome, outcome)
			assert.Equal(t, tc.wantBet, bet)
		})
	}
}

func TestGambleWinsBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		roll, chance int64
		want         bool
	}{
		{"the boundary roll wins", 50, 50, true},
		{"one past the boundary loses", 51, 50, false},
		{"one-in-a-hundred still has its one", 1, 1, true},
		{"one-in-a-hundred loses beyond it", 2, 1, false},
		{"always-win pays always", 100, 100, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, GambleWins(tc.roll, tc.chance))
		})
	}
}

func TestClampDuelSeconds(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int64
	}{
		{"unset takes the default", 0, 60},
		{"below the floor clamps up", 2, 10},
		{"above the ceiling clamps down", 99999, 1800},
		{"in range is kept", 45, 45},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ClampDuelSeconds(tc.in, DuelDefaultPotSeconds))
		})
	}
}

func TestDuelWinnerPickIsCanonicalAndBounded(t *testing.T) {
	stakes := SortDuelStakes([]DuelStake{
		{Login: "zoe", Stake: 10},
		{Login: "alice", Stake: 30},
		{Login: "bob", Stake: 20},
	})
	require.Equal(t, []DuelStake{{"alice", 30}, {"bob", 20}, {"zoe", 10}}, stakes,
		"canonical order is login-sorted regardless of input order")

	cases := []struct {
		name   string
		stakes []DuelStake
		roll   int64
		want   string
	}{
		{"the start of the first stake", stakes, 0, "alice"},
		{"the end of the first stake", stakes, 29, "alice"},
		{"the boundary lands on the next stake", stakes, 30, "bob"},
		{"the last roll in range", stakes, 59, "zoe"},
		{"out-of-range rolls fall off the end, never panic", stakes, 60, "zoe"},
		{"an empty pool picks nobody", nil, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, PickDuelWinner(tc.stakes, tc.roll))
		})
	}
}

func TestDigestDuelPoolBindsToThePool(t *testing.T) {
	a := SortDuelStakes([]DuelStake{{Login: "bob", Stake: 20}, {Login: "alice", Stake: 30}})
	b := SortDuelStakes([]DuelStake{{Login: "alice", Stake: 30}, {Login: "bob", Stake: 20}})
	c := SortDuelStakes([]DuelStake{{Login: "alice", Stake: 31}, {Login: "bob", Stake: 20}})

	assert.Equal(t, DigestDuelPool(a), DigestDuelPool(b), "digest binds to the pool, not the iteration order")
	assert.NotEqual(t, DigestDuelPool(a), DigestDuelPool(c), "a changed stake changes the digest")
}
