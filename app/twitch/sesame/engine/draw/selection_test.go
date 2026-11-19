// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw_test

import (
	"context"
	"math/rand"
	"strconv"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine/draw"

	"github.com/stretchr/testify/require"
)

type cohortVerifier struct{ eligible int }

func (v cohortVerifier) Verify(_ context.Context, check draw.Check) (draw.Verdict, error) {
	index, err := strconv.Atoi(string(check.Viewer))
	return draw.Verdict{Pass: index < v.eligible, Reason: "ineligible"}, err
}

type winnerSamples chan []draw.Slot

func (samples winnerSamples) Record(_ context.Context, _ draw.Round, slots []draw.Slot) error {
	samples <- slots
	return nil
}

func TestDrawRejectionSamplingUniformity(t *testing.T) {
	const (
		rounds      = 200
		perRound    = 100
		eligible    = 200
		rejected    = 30
		cohortCount = 10
	)
	random := rand.New(rand.NewSource(602))
	samples := make(winnerSamples, 1)
	f := newFixture(t, draw.Config{Random: random.Int63n, Verifier: cohortVerifier{eligible}, Winners: samples})
	f.start(f.Store)
	totals := make([]int, cohortCount)
	for range rounds {
		r := f.open(405, draw.Terms{Kind: draw.KindGiveaway, Winners: draw.WinnerPolicy{Count: perRound}})
		for i := range eligible + rejected {
			f.join(r, draw.ViewerID(strconv.Itoa(i)))
		}
		f.close(r.Channel)
		slots := take(t, samples)
		require.Len(t, slots, perRound)
		addSample(t, slots, totals)
	}
	finitePopulation := float64(eligible-perRound) / float64(eligible-1)
	require.Less(t, chiSquared(totals, rounds*perRound)/finitePopulation, 27.88)
}

func addSample(t *testing.T, slots []draw.Slot, totals []int) {
	t.Helper()
	var rejectCount int
	seen := make(map[draw.ViewerID]bool)
	for _, slot := range slots {
		index, err := strconv.Atoi(string(slot.Viewer))
		require.NoError(t, err)
		require.Less(t, index, 200)
		require.NotContains(t, seen, slot.Viewer)
		seen[slot.Viewer] = true
		totals[index%len(totals)]++
		rejectCount += len(slot.History) - 1
	}
	require.Positive(t, rejectCount)
}

func chiSquared(totals []int, count int) float64 {
	expect := float64(count) / float64(len(totals))
	var sum float64
	for _, n := range totals {
		diff := float64(n) - expect
		sum += diff * diff / expect
	}
	return sum
}
