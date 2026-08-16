// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The upstream allowance is a ROLLING window: a token bucket with burst B and
// refill r admits up to B + r*window per window, so the strict derivation must
// keep that sum at or under the limit — never the naive capacity=limit sizing
// that admits double.
func TestStrictBucketNeverExceedsWindow(t *testing.T) {
	for _, tc := range []struct {
		limit, window float64
	}{
		{300, 300},  // hypixel: 300 requests / 5 min
		{600, 300},  // urchin: 600 requests / 5 min
		{500, 600},  // mcsr: 500 requests / 10 min
		{225, 300},  // hypixel standard lane (75%)
		{301, 300},  // odd limit
		{100.3, 60}, // fractional config value
	} {
		burst, refill := strictBucket(tc.limit, tc.window)
		worstWindow := burst + refill*tc.window
		assert.LessOrEqualf(t, worstWindow, tc.limit,
			"limit %v/%vs: worst-case window %v must not exceed the allowance", tc.limit, tc.window, worstWindow)
		assert.GreaterOrEqual(t, burst, 1.0)
		assert.Greater(t, refill, 0.0)
		assert.Equal(t, burst, float64(int64(burst)), "burst must be integral for NewSpec")
	}
}

// Hypixel's 300/5min budget: half burst, half refill; the standard lane keeps
// 75% of the total so premium always has a 25% reserve.
func TestStrictBucketHypixelNumbers(t *testing.T) {
	burst, refill := strictBucket(300, 300)
	assert.Equal(t, 150.0, burst)
	assert.InDelta(t, 0.5, refill, 1e-9) // 150 more over the 5-minute window

	stdBurst, stdRefill := strictBucket(225, 300) // floor(300*0.75)
	assert.Equal(t, 112.0, stdBurst)
	// Standard lane worst window: 112 + 113 = 225 = 75% of the allowance.
	assert.InDelta(t, 225.0, stdBurst+stdRefill*300, 1e-9)
}

// A degenerate tiny budget still builds a valid spec (positive refill) rather
// than panicking NewSpec; strictness yields on configs that small.
func TestStrictBucketDegenerateBudget(t *testing.T) {
	burst, refill := strictBucket(1, 300)
	assert.Equal(t, 1.0, burst)
	assert.Greater(t, refill, 0.0)
}

// Odd configured limits (including the 0.75 derivation) must build, not panic.
func TestNewBucketsDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		NewBuckets("k", 300, 300)
		NewBuckets("k", 550.5, 300)
		NewBuckets("k", 1, 300)
		NewBuckets("k", 0, 300) // clamped to 1 by the caller-facing floor
	})
}

// A paced bucket must satisfy BOTH constraints at once: the rolling window
// stays at the documented quota, and the instantaneous burst stays at the
// measured edge ceiling. Coral's numbers: 600/5min quota, ~40-request ~4/s
// edge wall; burst 8 + ~1.97/s spends the full quota without ever bursting
// near the wall.
func TestPacedBucketCoralNumbers(t *testing.T) {
	burst, refill := pacedBucket(600, 300, 8)
	assert.Equal(t, 8.0, burst)
	assert.InDelta(t, (600.0-8.0)/300.0, refill, 1e-9)
	assert.InDelta(t, 600.0, burst+refill*300, 1e-9, "full quota still spent across the rolling window")
	assert.Less(t, refill, 4.0, "sustained pace must stay under the measured edge refill")

	// Standard lane: 75% of quota, 75% of burst ceiling.
	stdBurst, stdRefill := pacedBucket(450, 300, 6)
	assert.Equal(t, 6.0, stdBurst)
	assert.InDelta(t, 450.0, stdBurst+stdRefill*300, 1e-9)
}

// A ceiling wider than the strict half-split must not loosen the rolling
// window: pacedBucket clamps to the strict burst.
func TestPacedBucketClampsToStrict(t *testing.T) {
	burst, refill := pacedBucket(300, 300, 1000)
	sBurst, sRefill := strictBucket(300, 300)
	assert.Equal(t, sBurst, burst)
	assert.InDelta(t, sRefill, refill, 1e-9)
}

func TestNewPacedBucketsDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		NewPacedBuckets("k", 600, 300, 8)
		NewPacedBuckets("k", 1, 300, 8)
		NewPacedBuckets("k", 600.7, 300, 0.4) // ceiling under 1 clamps to 1
	})
}
