// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStrictBucketNeverExceedsWindow(t *testing.T) {
	for _, tc := range []struct {
		limit, window float64
	}{
		{300, 300},
		{600, 300},
		{500, 600},
		{225, 300},
		{301, 300},
		{100.3, 60},
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

func TestStrictBucketHypixelNumbers(t *testing.T) {
	burst, refill := strictBucket(300, 300)
	assert.Equal(t, 150.0, burst)
	assert.InDelta(t, 0.5, refill, 1e-9)

	stdBurst, stdRefill := strictBucket(225, 300)
	assert.Equal(t, 112.0, stdBurst)
	assert.InDelta(t, 225.0, stdBurst+stdRefill*300, 1e-9)
}

func TestStrictBucketDegenerateBudget(t *testing.T) {
	burst, refill := strictBucket(1, 300)
	assert.Equal(t, 1.0, burst)
	assert.Greater(t, refill, 0.0)
}

func TestNewBucketsDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		NewBuckets("k", 300, 300)
		NewBuckets("k", 550.5, 300)
		NewBuckets("k", 1, 300)
		NewBuckets("k", 0, 300)
	})
}

func TestPacedBucketCoralNumbers(t *testing.T) {
	burst, refill := pacedBucket(600, 300, 8)
	assert.Equal(t, 8.0, burst)
	assert.InDelta(t, (600.0-8.0)/300.0, refill, 1e-9)
	assert.InDelta(t, 600.0, burst+refill*300, 1e-9, "full quota still spent across the rolling window")
	assert.Less(t, refill, 4.0, "sustained pace must stay under the measured edge refill")

	stdBurst, stdRefill := pacedBucket(450, 300, 6)
	assert.Equal(t, 6.0, stdBurst)
	assert.InDelta(t, 450.0, stdBurst+stdRefill*300, 1e-9)
}

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
		NewPacedBuckets("k", 600.7, 300, 0.4)
	})
}
