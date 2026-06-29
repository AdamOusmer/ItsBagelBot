package ratelimit

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// tokenBucket is a minimal token bucket with explicit-time refill. It carries no
// lock of its own: every access goes through LocalBucket.mu, which already
// serializes admission, so the redundant per-limiter mutex of x/time/rate is
// removed from the hot path. refill advancing on a denied request is equivalent
// to leaving it untouched because accrual is linear and capped, so admission
// never mints tokens.
type tokenBucket struct {
	tokens float64
	last   time.Time
	rate   float64 // tokens per second
	burst  float64
}

func (t *tokenBucket) init(now time.Time, ratePerSec float64, burst int) {
	t.rate = ratePerSec
	t.burst = float64(burst)
	t.tokens = 0 // a new incarnation always starts empty
	t.last = now
}

// refill advances the bucket to now and returns the current token count.
func (t *tokenBucket) refill(now time.Time) float64 {
	if now.After(t.last) {
		t.tokens += now.Sub(t.last).Seconds() * t.rate
		if t.tokens > t.burst {
			t.tokens = t.burst
		}
		t.last = now
	}
	return t.tokens
}

// TokensAt reports the token count at now without mutating the bucket.
func (t *tokenBucket) TokensAt(now time.Time) float64 {
	tokens := t.tokens
	if now.After(t.last) {
		tokens += now.Sub(t.last).Seconds() * t.rate
		if tokens > t.burst {
			tokens = t.burst
		}
	}
	return tokens
}

func (t *tokenBucket) allow(now time.Time) bool {
	if t.refill(now) < 1 {
		return false
	}
	t.tokens--
	return true
}

func (t *tokenBucket) setRate(now time.Time, ratePerSec float64) {
	t.refill(now)
	t.rate = ratePerSec
}

// setBurst resizes the cap. Raising the cap never grants tokens on its own;
// lowering it clamps the current balance.
func (t *tokenBucket) setBurst(now time.Time, burst int) {
	t.refill(now)
	t.burst = float64(burst)
	if t.tokens > t.burst {
		t.tokens = t.burst
	}
}

// LocalBucket represents a local in-process token bucket for rate limiting.
// Admission is serialized by mu; the embedded token buckets are plain structs.
type LocalBucket struct {
	mu          sync.Mutex
	epoch       uint64
	generation  uint64
	holder      string
	shared      tokenBucket
	standard    tokenBucket
	hasShared   bool
	hasStandard bool
	notBefore   time.Time // local monotonic deadline
	notAfter    time.Time // local monotonic deadline
	config      atomic.Uint64
}

// NewLocalBucket creates an uninitialized local bucket.
func NewLocalBucket() *LocalBucket {
	return &LocalBucket{}
}

// Update updates the bucket configuration. If the holder changes, the limiters are recreated
// and drained immediately to avoid minting bursts. If only the burst/rate change, they are updated in place.
func (b *LocalBucket) Update(now time.Time, epoch, generation uint64, holder string, notBefore, notAfter time.Time, sharedRate rate.Limit, sharedBurst int, standardRate rate.Limit, standardBurst int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if generation == 0 || holder == "" || !notBefore.Before(notAfter) || sharedRate <= 0 || sharedBurst <= 0 || standardBurst < 0 || standardBurst > 0 && standardRate <= 0 {
		b.epoch = 0
		b.generation = 0
		b.holder = ""
		b.hasShared = false
		b.hasStandard = false
		b.shared = tokenBucket{}
		b.standard = tokenBucket{}
		b.notBefore = time.Time{}
		b.notAfter = time.Time{}
		b.config.Store(0)
		return
	}

	incarnationChanged := b.holder != holder || b.generation != generation
	b.epoch = epoch
	b.generation = generation
	b.notBefore = notBefore
	b.notAfter = notAfter
	b.holder = holder

	if !b.hasShared || incarnationChanged {
		b.shared.init(now, float64(sharedRate), sharedBurst) // new incarnations always start empty
		b.hasShared = true
	} else {
		b.shared.setRate(now, float64(sharedRate))
		b.shared.setBurst(now, sharedBurst)
	}

	if standardBurst > 0 {
		if !b.hasStandard || incarnationChanged {
			b.standard.init(now, float64(standardRate), standardBurst)
			b.hasStandard = true
		} else {
			b.standard.setRate(now, float64(standardRate))
			b.standard.setBurst(now, standardBurst)
		}
	} else {
		b.hasStandard = false
		b.standard = tokenBucket{}
	}
	b.config.Store(bucketConfigSignature(sharedRate, sharedBurst, standardRate, standardBurst))
}

// MatchesConfig is an allocation-free optimistic check used before admission.
// Update still serializes all actual limiter changes with admission under mu.
func (b *LocalBucket) MatchesConfig(sharedRate rate.Limit, sharedBurst int, standardRate rate.Limit, standardBurst int) bool {
	return b.config.Load() == bucketConfigSignature(sharedRate, sharedBurst, standardRate, standardBurst)
}

// MatchesSignature compares against a precomputed bucketConfigSignature so the
// admission hot path avoids recomputing the float-bits mix on every call.
func (b *LocalBucket) MatchesSignature(signature uint64) bool {
	return b.config.Load() == signature
}

func bucketConfigSignature(sharedRate rate.Limit, sharedBurst int, standardRate rate.Limit, standardBurst int) uint64 {
	// The inputs are process-derived, not attacker-controlled. This mix is an
	// identity hint; Update remains the synchronization and validation point.
	signature := math.Float64bits(float64(sharedRate))
	signature ^= math.Float64bits(float64(standardRate)) * 0x9e3779b97f4a7c15
	signature ^= uint64(sharedBurst) * 0xbf58476d1ce4e5b9
	signature ^= uint64(standardBurst) * 0x94d049bb133111eb
	if signature == 0 {
		return 1
	}
	return signature
}

// Renew updates the bucket's epoch and validity without modifying the underlying limiters.
func (b *LocalBucket) Renew(epoch uint64, notBefore, notAfter time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.epoch = epoch
	b.notBefore = notBefore
	b.notAfter = notAfter
}

// TryPremium attempts to consume a token from the shared bucket.
// It returns true if successful.
func (b *LocalBucket) TryPremium(now time.Time) bool {
	allowed, _ := b.TryPremiumLease(now, 0, 0)
	return allowed
}

// TryPremiumLease returns stale when the bucket belongs to a different plan
// incarnation. The common hit performs one outer lock and one x/time/rate call.
func (b *LocalBucket) TryPremiumLease(now time.Time, epoch, generation uint64) (allowed, stale bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if epoch != 0 {
		if b.epoch != epoch || b.generation != generation {
			return false, true
		}
		// A matching incarnation means the bucket window equals the active plan
		// window, which the caller already validated; skip re-checking it here.
	} else if now.Before(b.notBefore) || !now.Before(b.notAfter) {
		return false, false
	}

	if !b.hasShared {
		return false, false
	}

	return b.shared.allow(now), false
}

// TryStandard attempts to consume a token from the standard bucket first,
// and if successful, attempts to consume a token from the shared bucket.
// It returns two booleans: (standardPaid, sharedPaid).
// If the shared bucket denies the request, no tokens are consumed from either bucket.
func (b *LocalBucket) TryStandard(now time.Time) (bool, bool) {
	standard, shared, _ := b.TryStandardLease(now, 0, 0)
	return standard, shared
}

func (b *LocalBucket) TryStandardLease(now time.Time, epoch, generation uint64) (standard, shared, stale bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if epoch != 0 {
		if b.epoch != epoch || b.generation != generation {
			return false, false, true
		}
		// A matching incarnation means the bucket window equals the active plan
		// window, which the caller already validated; skip re-checking it here.
	} else if now.Before(b.notBefore) || !now.Before(b.notAfter) {
		return false, false, false
	}

	if !b.hasStandard || !b.hasShared {
		return false, false, false
	}

	// Check both while holding the outer bucket lock. No premium request can
	// interleave between the checks and the two debits. refill advances both,
	// but a token is only spent when both partitions have capacity.
	if b.standard.refill(now) < 1 {
		return false, false, false // standard denied
	}
	if b.shared.refill(now) < 1 {
		return false, false, false
	}

	b.standard.tokens--
	b.shared.tokens--
	return true, true, false
}

// IsValid checks if the bucket is active for the given time.
func (b *LocalBucket) IsValid(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !now.Before(b.notBefore) && now.Before(b.notAfter)
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

func (b *LocalBucket) Generation() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.generation
}

func (b *LocalBucket) ExpiredUnixNano(now int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.notAfter.IsZero() || b.notAfter.UnixNano() <= now
}
