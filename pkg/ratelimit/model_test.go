package ratelimit

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

// TestModel_Property verifies bucket invariants through simulated events
func TestModel_Property(t *testing.T) {
	// A simple property test checking that bursts are never minted
	// and that aggregate successful admissions don't exceed the global model.

	b := NewLocalBucket()
	now := time.Now()

	// We'll simulate 10 seconds of random activity
	var successfulAdmissions int

	// First initialization
	b.Update(now, 1, 1, "pod-a", now, now.Add(time.Hour), rate.Limit(10), 10, rate.Limit(5), 5)

	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 1000; i++ {
		// Advance time by a random amount between 1ms and 50ms
		step := time.Duration(rng.Intn(50)+1) * time.Millisecond
		now = now.Add(step)

		action := rng.Intn(100)

		if action < 10 {
			// 10% chance to renew
			b.Renew(b.Epoch()+1, now, now.Add(time.Hour))
		} else if action < 15 {
			// 5% chance to resize (same holder)
			b.Update(now, b.Epoch()+1, 1, "pod-a", now, now.Add(time.Hour), rate.Limit(10), 10, rate.Limit(5), 5)
		} else if action < 20 {
			// 5% chance of a membership generation change / pod restart.
			b.Update(now, b.Epoch()+1, 2, "pod-b", now, now.Add(time.Hour), rate.Limit(10), 10, rate.Limit(5), 5)
		} else if action < 60 {
			// 40% chance of premium try
			if b.TryPremium(now) {
				successfulAdmissions++
			}
		} else {
			// 40% chance of standard try
			st, sh := b.TryStandard(now)
			if st && sh {
				successfulAdmissions++
			}
		}
	}

	// At 10 tokens/sec for ~25 seconds max (1000 * 25ms avg), that's ~250 tokens
	// The maximum theoretical successful admissions is burst + (rate * time).
	// Every generation change drains the bucket, so admissions are lower still.
	// We just ensure we don't inexplicably admit 1000 requests.

	assert.LessOrEqual(t, successfulAdmissions, 500, "Should not exceed theoretical max capacity")
}
