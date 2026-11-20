// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"

	"github.com/stretchr/testify/require"
)

func TestDrawPolicyPins(t *testing.T) {
	require.Equal(t, draw.PolicyPins{
		EntryWeight: 100, MaxWinners: 100, GiveawayDefaultWinners: 1, RaffleDefaultWinners: 1,
		GiveawayEntryWindow: 3 * time.Minute, GiveawayMaxPresets: 10, GiveawayPresetNameMax: 24,
		MultiRaffleDefaultWinners: 3, AutoDivisor: 10,
		ClaimWindowMin: 30 * time.Second, ClaimWindowMax: 15 * time.Minute, ClaimWindowDefault: 2 * time.Minute,
		PayoutHorizon: 24 * time.Hour, VerifyingNotice: 10 * time.Second, VerifyStuck: 2 * time.Minute,
		ModActionTimeout: 30 * time.Minute, GiveawayHorizon: 24 * time.Hour, RemindInterval: 5 * time.Minute,
		MaxOpen: 2 * time.Hour, WinnerCooldown: 5 * time.Second,
	}, draw.Pins)
}

func TestAutoWinnersNeverDecrease(t *testing.T) {
	cases := []struct {
		prize int64
		want  []int
	}{
		{1, []int{1, 1, 1, 1, 1, 1, 1}},
		{2, []int{1, 1, 1, 2, 2, 2, 2}},
		{5000, []int{1, 1, 1, 2, 3, 100, 100}},
	}
	policy := draw.WinnerPolicy{Auto: true}
	for _, tc := range cases {
		prize := draw.Prize{Points: tc.prize}
		sizes := []int{0, 1, 10, 11, 21, 1000, 10_000}
		var got []int
		for _, size := range sizes {
			got = append(got, draw.ResolveWinners(policy, size, prize))
		}
		require.Equal(t, tc.want, got)
	}
}
