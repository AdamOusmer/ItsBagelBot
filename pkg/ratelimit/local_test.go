package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestLocalBucket_Update_StartsEmpty(t *testing.T) {
	b := NewLocalBucket()
	now := time.Now()

	b.Update(now, 1, 1, "pod1", now.Add(-time.Second), now.Add(time.Second), rate.Limit(10), 10, rate.Limit(5), 5)

	// Since it's a new holder, it should be drained immediately
	// TryPremium should fail because 10 burst was drained
	assert.False(t, b.TryPremium(now), "Expected premium to be empty upon creation")

	st, sh := b.TryStandard(now)
	assert.False(t, st, "Expected standard to be empty upon creation")
	assert.False(t, sh)

	// Wait 100ms to get 1 token in both
	later := now.Add(100 * time.Millisecond)
	assert.True(t, b.TryPremium(later))
}

func TestLocalBucket_TryStandard_Fallback(t *testing.T) {
	b := NewLocalBucket()
	now := time.Now()

	b.Update(now, 1, 1, "pod1", now.Add(-time.Second), now.Add(time.Hour), rate.Limit(10), 10, rate.Limit(5), 5)

	// let it refill to full
	later := now.Add(2 * time.Second)

	// Consume 5 standard tokens (which consumes 5 shared tokens)
	for i := 0; i < 5; i++ {
		st, sh := b.TryStandard(later)
		assert.True(t, st)
		assert.True(t, sh)
	}

	// Next try should fail standard, and thus NOT consume shared
	st, sh := b.TryStandard(later)
	assert.False(t, st)
	assert.False(t, sh)

	// Try Premium directly (which uses shared bucket), should succeed since shared burst was 10 and we only used 5
	assert.True(t, b.TryPremium(later))
}

func TestLocalBucket_Validity(t *testing.T) {
	b := NewLocalBucket()
	now := time.Now()

	notBefore := now.Add(time.Second)
	notAfter := now.Add(2 * time.Second)

	b.Update(now, 1, 1, "pod1", notBefore, notAfter, rate.Limit(10), 10, rate.Limit(5), 5)

	// Too early
	assert.False(t, b.TryPremium(now))
	st, sh := b.TryStandard(now)
	assert.False(t, st)
	assert.False(t, sh)

	// Just right (1.5 seconds in)
	later := now.Add(1500 * time.Millisecond)
	assert.True(t, b.TryPremium(later))

	// Too late
	tooLate := now.Add(3 * time.Second)
	assert.False(t, b.TryPremium(tooLate))
}

func TestLocalBucket_Renew(t *testing.T) {
	b := NewLocalBucket()
	now := time.Now()

	b.Update(now, 1, 1, "pod1", now.Add(-time.Second), now.Add(time.Second), rate.Limit(10), 10, rate.Limit(5), 5)

	// Fast forward to get some tokens
	later := now.Add(500 * time.Millisecond)

	b.Renew(2, now, later.Add(time.Second))
	assert.Equal(t, uint64(2), b.Epoch())

	// Ensure tokens are still available (didn't reset)
	assert.True(t, b.TryPremium(later))
}

func TestLocalBucket_Resize(t *testing.T) {
	b := NewLocalBucket()
	now := time.Now()

	b.Update(now, 1, 1, "pod1", now.Add(-time.Second), now.Add(time.Hour), rate.Limit(10), 10, rate.Limit(5), 5)

	later := now.Add(time.Second)

	// Update with new rate/burst, but same holder. Should not drain tokens.
	b.Update(later, 2, 1, "pod1", later, later.Add(time.Hour), rate.Limit(20), 20, rate.Limit(10), 10)

	// Tokens should be available immediately
	assert.True(t, b.TryPremium(later))
}
