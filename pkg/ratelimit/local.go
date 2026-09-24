// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// No lock of its own: LocalBucket.mu guards every access.
type tokenBucket struct {
	tokens     float64
	last       time.Time
	ratePerSec float64
	burst      float64
}

func (t *tokenBucket) init(now time.Time, ratePerSec float64, burst int) {
	t.ratePerSec = ratePerSec
	t.burst = float64(burst)
	t.tokens = 0
	t.last = now
}

func (t *tokenBucket) refill(now time.Time) float64 {
	if now.After(t.last) {
		t.tokens += now.Sub(t.last).Seconds() * t.ratePerSec
		if t.tokens > t.burst {
			t.tokens = t.burst
		}
		t.last = now
	}
	return t.tokens
}

func (t *tokenBucket) TokensAt(now time.Time) float64 {
	tokens := t.tokens
	if now.After(t.last) {
		tokens += now.Sub(t.last).Seconds() * t.ratePerSec
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
	t.ratePerSec = ratePerSec
}

func (t *tokenBucket) setBurst(now time.Time, burst int) {
	t.refill(now)
	t.burst = float64(burst)
	if t.tokens > t.burst {
		t.tokens = t.burst
	}
}

type LocalBucket struct {
	mu          sync.Mutex
	epoch       uint64
	generation  uint64
	holder      string
	shared      tokenBucket
	standard    tokenBucket
	hasShared   bool
	hasStandard bool
	notBefore   time.Time
	notAfter    time.Time
	config      atomic.Uint64
}

func NewLocalBucket() *LocalBucket {
	return &LocalBucket{}
}

type BucketConfig struct {
	Epoch         uint64
	Generation    uint64
	Holder        string
	NotBefore     time.Time
	NotAfter      time.Time
	SharedRate    rate.Limit
	SharedBurst   int
	StandardRate  rate.Limit
	StandardBurst int
}

func (c BucketConfig) valid() bool {
	if c.Generation == 0 || c.Holder == "" {
		return false
	}
	if !c.NotBefore.Before(c.NotAfter) {
		return false
	}
	if c.SharedRate <= 0 || c.SharedBurst <= 0 {
		return false
	}
	if c.StandardBurst < 0 {
		return false
	}
	return c.StandardBurst == 0 || c.StandardRate > 0
}

func (b *LocalBucket) Update(now time.Time, cfg BucketConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !cfg.valid() {
		b.reset()
		return
	}

	incarnationChanged := b.holder != cfg.Holder || b.generation != cfg.Generation
	b.epoch = cfg.Epoch
	b.generation = cfg.Generation
	b.notBefore = cfg.NotBefore
	b.notAfter = cfg.NotAfter
	b.holder = cfg.Holder

	b.applyShared(now, cfg, incarnationChanged)
	b.applyStandard(now, cfg, incarnationChanged)
	b.config.Store(bucketConfigSignature(cfg.SharedRate, cfg.SharedBurst, cfg.StandardRate, cfg.StandardBurst))
}

func (b *LocalBucket) reset() {
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
}

func (b *LocalBucket) applyShared(now time.Time, cfg BucketConfig, incarnationChanged bool) {
	if !b.hasShared || incarnationChanged {
		b.shared.init(now, float64(cfg.SharedRate), cfg.SharedBurst)
		b.hasShared = true
		return
	}
	b.shared.setRate(now, float64(cfg.SharedRate))
	b.shared.setBurst(now, cfg.SharedBurst)
}

func (b *LocalBucket) applyStandard(now time.Time, cfg BucketConfig, incarnationChanged bool) {
	if cfg.StandardBurst <= 0 {
		b.hasStandard = false
		b.standard = tokenBucket{}
		return
	}
	if !b.hasStandard || incarnationChanged {
		b.standard.init(now, float64(cfg.StandardRate), cfg.StandardBurst)
		b.hasStandard = true
		return
	}
	b.standard.setRate(now, float64(cfg.StandardRate))
	b.standard.setBurst(now, cfg.StandardBurst)
}

func (b *LocalBucket) MatchesConfig(sharedRate rate.Limit, sharedBurst int, standardRate rate.Limit, standardBurst int) bool {
	return b.config.Load() == bucketConfigSignature(sharedRate, sharedBurst, standardRate, standardBurst)
}

func (b *LocalBucket) MatchesSignature(signature uint64) bool {
	return b.config.Load() == signature
}

func bucketConfigSignature(sharedRate rate.Limit, sharedBurst int, standardRate rate.Limit, standardBurst int) uint64 {
	signature := math.Float64bits(float64(sharedRate))
	signature ^= math.Float64bits(float64(standardRate)) * 0x9e3779b97f4a7c15
	signature ^= uint64(sharedBurst) * 0xbf58476d1ce4e5b9
	signature ^= uint64(standardBurst) * 0x94d049bb133111eb
	if signature == 0 {
		return 1
	}
	return signature
}

func (b *LocalBucket) Renew(epoch uint64, notBefore, notAfter time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.epoch = epoch
	b.notBefore = notBefore
	b.notAfter = notAfter
}

func (b *LocalBucket) TryPremium(now time.Time) bool {
	allowed, _ := b.TryPremiumLease(now, 0, 0)
	return allowed
}

func (b *LocalBucket) windowValid(now time.Time, epoch, generation uint64) (ok, stale bool) {
	if epoch != 0 {
		if b.epoch != epoch || b.generation != generation {
			return false, true
		}
		return true, false
	}
	if now.Before(b.notBefore) || !now.Before(b.notAfter) {
		return false, false
	}
	return true, false
}

func (b *LocalBucket) TryPremiumLease(now time.Time, epoch, generation uint64) (allowed, stale bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ok, stale := b.windowValid(now, epoch, generation); !ok {
		return false, stale
	}
	if !b.hasShared {
		return false, false
	}
	return b.shared.allow(now), false
}

func (b *LocalBucket) TryStandard(now time.Time) (bool, bool) {
	standard, shared, _ := b.TryStandardLease(now, 0, 0)
	return standard, shared
}

func (b *LocalBucket) TryStandardLease(now time.Time, epoch, generation uint64) (standard, shared, stale bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ok, stale := b.windowValid(now, epoch, generation); !ok {
		return false, false, stale
	}

	if !b.hasStandard || !b.hasShared {
		return false, false, false
	}

	if b.standard.refill(now) < 1 {
		return false, false, false
	}
	if b.shared.refill(now) < 1 {
		return false, false, false
	}

	b.standard.tokens--
	b.shared.tokens--
	return true, true, false
}

func (b *LocalBucket) IsValid(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !now.Before(b.notBefore) && now.Before(b.notAfter)
}

func (b *LocalBucket) Holder() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holder
}

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
