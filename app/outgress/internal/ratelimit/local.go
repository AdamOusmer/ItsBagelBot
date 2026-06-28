package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// LocalBucket represents a local in-process token bucket for rate limiting.
// It uses x/time/rate for admission control and is concurrency-safe.
type LocalBucket struct {
	mu        sync.Mutex
	epoch     uint64
	holder    string
	shared    *rate.Limiter
	standard  *rate.Limiter
	notBefore time.Time // local monotonic deadline
	notAfter  time.Time // local monotonic deadline
}

// NewLocalBucket creates an uninitialized local bucket.
func NewLocalBucket() *LocalBucket {
	return &LocalBucket{}
}

// Update updates the bucket configuration. If the holder changes, the limiters are recreated
// and drained immediately to avoid minting bursts. If only the burst/rate change, they are updated in place.
func (b *LocalBucket) Update(now time.Time, epoch uint64, holder string, notBefore, notAfter time.Time, sharedRate rate.Limit, sharedBurst int, standardRate rate.Limit, standardBurst int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.epoch = epoch
	b.notBefore = notBefore
	b.notAfter = notAfter

	holderChanged := b.holder != holder
	b.holder = holder

	if b.shared == nil || holderChanged {
		b.shared = rate.NewLimiter(sharedRate, sharedBurst)
		b.shared.AllowN(now, sharedBurst) // Start empty to avoid minting a burst
	} else {
		b.shared.SetLimitAt(now, sharedRate)
		b.shared.SetBurstAt(now, sharedBurst)
	}

	if standardBurst > 0 {
		if b.standard == nil || holderChanged {
			b.standard = rate.NewLimiter(standardRate, standardBurst)
			b.standard.AllowN(now, standardBurst) // Start empty to avoid minting a burst
		} else {
			b.standard.SetLimitAt(now, standardRate)
			b.standard.SetBurstAt(now, standardBurst)
		}
	} else {
		b.standard = nil
	}
}

// Renew updates the bucket's epoch and validity without modifying the underlying limiters.
func (b *LocalBucket) Renew(epoch uint64, notAfter time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.epoch = epoch
	b.notAfter = notAfter
}

// TryPremium attempts to consume a token from the shared bucket.
// It returns true if successful.
func (b *LocalBucket) TryPremium(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if now.Before(b.notBefore) || now.After(b.notAfter) {
		return false
	}

	if b.shared == nil {
		return false
	}

	return b.shared.AllowN(now, 1)
}

// TryStandard attempts to consume a token from the standard bucket first,
// and if successful, attempts to consume a token from the shared bucket.
// It returns two booleans: (standardPaid, sharedPaid).
// If shared denies, the standard token remains consumed.
func (b *LocalBucket) TryStandard(now time.Time) (bool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if now.Before(b.notBefore) || now.After(b.notAfter) {
		return false, false
	}

	if b.standard == nil || b.shared == nil {
		return false, false
	}

	standardPaid := b.standard.AllowN(now, 1)
	if !standardPaid {
		return false, false
	}

	sharedPaid := b.shared.AllowN(now, 1)
	return true, sharedPaid
}

// IsValid checks if the bucket is active for the given time.
func (b *LocalBucket) IsValid(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !now.Before(b.notBefore) && !now.After(b.notAfter)
}

// Holder returns the current holder identity.
func (b *LocalBucket) Holder() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holder
}

// Epoch returns the current epoch.
func (b *LocalBucket) Epoch() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.epoch
}
