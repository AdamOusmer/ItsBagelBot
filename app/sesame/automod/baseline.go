// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package automod's adaptive style layer. This file holds Baseline: a
// per-channel EWMA of the style ratios the gate already computes, used to
// RAISE the fixed caps/symbol/token-len thresholds for channels whose house
// style is louder than the fleet default.
//
// Wiring (since 2026-08-23): main.go builds one Baseline with DefaultCeiling
// and installs it via Gate.SetBaseline; gate.Assess observes every judged
// line (observeLearned in learned.go) BEFORE verdict resolution and resolves
// caps/symbol against Baseline.Adjust(ch, kind, staticThreshold) inside
// resolveStyle. The profile-resolved static values stay authoritative as
// floors on BOTH the cold and warm paths, so wiring this in cannot flip a
// single pre-existing verdict until ~coldFloorN judged lines accumulate -
// and can then only raise thresholds, never lower them.
package automod

import (
	"math"
	"sort"
	"sync"
	"time"
)

// Tuned constants. Every number here traces to the 2026-08-22 shadow-mode
// audit (precision 0%: all 8 heuristic detections were reaction-noise false
// positives like "LUL LUL LUL LUL") or to bot_stats.go's memory discipline.
const (
	// baselineShards spreads lock contention across chat traffic; 64 keeps a
	// shard header under one cache-line pair and collision odds negligible at
	// fleet scale. Same shape as Vocab's shards so reviewers read them alike.
	baselineShards    = 64
	baselineShardMask = baselineShards - 1

	// ewmaAlpha 0.05 = ~20-line memory. The audit showed FP storms come in
	// bursts (hype moments), so the baseline must not chase a single loud
	// minute: at 0.05 a channel needs sustained shouting to move its mean.
	ewmaAlpha = 0.05

	// zScore 2.0 ≈ 97.7th percentile of the channel's own history: raise the
	// bar only for genuinely hotter-than-their-normal lines, not noise.
	zScore = 2.0

	// coldFloorN 50 judged lines before trusting a channel's stats. Below
	// this the variance estimate is garbage (a dozen messages), and the
	// evasion-resistance rule below forbids lowering anyway, so early stats
	// would only ever add risk of a bad raise.
	coldFloorN = 50

	// baselineChanCap mirrors bot_stats.go channelStatsMaxKeys=4096: a memory
	// backstop against pathological fan-out or a bug minting broadcaster ids,
	// not a working limit. Overflow drops the stalest half by lastSeen so a
	// returning active channel re-warms rather than staying evicted.
	baselineChanCap = 4096
)

// StyleKind selects which learned series Adjust consults.
type StyleKind uint8

const (
	KindCaps StyleKind = iota
	KindSymbol
	KindTokenLen
)

// Ceiling carries the absolute thresholds the gate applies today. A baseline
// may raise a threshold above these but NEVER below them: documented floors
// are evasion resistance — an attacker boiling the frog must stop at the
// published ceiling, not slide the gate open line by line.
type Ceiling struct {
	Caps     float64 // gate.go capsThreshold
	Symbol   float64 // gate.go symbolRatioHi
	TokenLen float64 // 0: gate.go documents no token-len ceiling today, so KindTokenLen adjusts advisory-only
}

// DefaultCeiling mirrors gate.go's constants at the time of writing.
func DefaultCeiling() Ceiling { return Ceiling{Caps: 0.7, Symbol: 0.6} }

// Baseline learns per-channel style distributions over judged lines.
// Build once at module wiring; all methods are goroutine-safe and allocate
// nothing on the steady-state Observe/Adjust path.
type Baseline struct {
	shards [baselineShards]struct {
		sync.Mutex
		m map[uint64]*chanStats
	}
	capsCeil, symCeil, tokCeil float64
	now                        func() int64 // unix seconds; injectable so decay-style tests stay deterministic
}

type chanStats struct {
	n              uint32
	lastSeen       int64
	caps, sym, tok metricEWMA
}

// metricEWMA tracks E[x] and E[x^2]; stddev = sqrt(E[x^2]-E[x]^2), clamped.
// Two FMAs per observation, no history buffer — the whole point is that a
// per-channel ring buffer would be 3 metrics × unbounded retention for the
// same information the second moment already carries (~100 lines beats
// importing a dependency; two floats beat both).
type metricEWMA struct{ mean, ex2 float64 }

func (e *metricEWMA) add(x float64) {
	e.mean += ewmaAlpha * (x - e.mean)
	e.ex2 += ewmaAlpha * (x*x - e.ex2)
}

func (e *metricEWMA) thresh() float64 {
	v := e.ex2 - e.mean*e.mean
	if v <= 0 {
		return e.mean
	}
	return e.mean + zScore*math.Sqrt(v)
}

// NewBaseline builds a Baseline; zero-valued Ceiling fields fall back to
// DefaultCeiling so a careless literal can't silently disable a floor.
func NewBaseline(c Ceiling) *Baseline {
	def := DefaultCeiling()
	if c.Caps <= 0 {
		c.Caps = def.Caps
	}
	if c.Symbol <= 0 {
		c.Symbol = def.Symbol
	}
	b := &Baseline{
		capsCeil: c.Caps,
		symCeil:  c.Symbol,
		tokCeil:  c.TokenLen,
		now:      func() int64 { return time.Now().Unix() },
	}
	for i := range b.shards {
		b.shards[i].m = make(map[uint64]*chanStats)
	}
	return b
}

// Observe folds one judged line's style measurements into channel ch's
// distribution. Call for every judged line, clean or struck.
func (b *Baseline) Observe(ch uint64, caps, symbol, tokens float64) {
	s := &b.shards[ch&baselineShardMask]
	s.Lock()
	cs := s.m[ch]
	if cs == nil {
		evictStalestHalf(s.m, func(cs *chanStats) int64 { return cs.lastSeen })
		cs = &chanStats{}
		s.m[ch] = cs
	}
	cs.n++
	cs.lastSeen = b.now()
	cs.caps.add(caps)
	cs.sym.add(symbol)
	cs.tok.add(tokens)
	s.Unlock()
}

// Adjust returns the EFFECTIVE threshold for kind on channel ch:
// max(ceiling[kind], mean + zScore·stddev) once the channel is warm
// (>= coldFloorN observations), else max(ceiling[kind], ratio). ratio is the
// static threshold the caller was about to apply and floors BOTH paths - on
// the cold path too, so a stricter per-channel config survives adaptation
// from line one and wiring this in cannot flip a single pre-existing verdict
// until ~coldFloorN judged lines accumulate. Direction is raise-only by
// construction: every branch bottoms out at a documented ceiling.
func (b *Baseline) Adjust(ch uint64, kind StyleKind, ratio float64) float64 {
	return b.floored(kind, b.rawThresh(ch, kind), ratio)
}

// rawThresh is the stats-lookup half of Adjust: kind's observed threshold for
// channel ch under the shard lock - the ceiling until the channel is warm
// (below coldFloorN observations the variance estimate is garbage), and
// mean+zScore*stddev of the learned series afterwards.
func (b *Baseline) rawThresh(ch uint64, kind StyleKind) float64 {
	s := &b.shards[ch&baselineShardMask]
	s.Lock()
	defer s.Unlock()
	cs := s.m[ch]
	if cs == nil || cs.n < coldFloorN {
		return b.ceilingOf(kind)
	}
	return kind.series(cs).thresh()
}

// series selects the EWMA Adjust consults for kind.
func (k StyleKind) series(cs *chanStats) *metricEWMA {
	switch k {
	case KindCaps:
		return &cs.caps
	case KindSymbol:
		return &cs.sym
	default:
		return &cs.tok
	}
}

// floored applies BOTH raise-only floors in one place - the documented
// ceiling for the series and the caller's static threshold - so every return
// path of Adjust (cold and warm alike) bottoms out at a documented ceiling.
func (b *Baseline) floored(kind StyleKind, t, ratio float64) float64 {
	if eff := b.ceilingOf(kind); t < eff {
		t = eff
	}
	if t < ratio {
		t = ratio
	}
	return t
}

func (b *Baseline) ceilingOf(kind StyleKind) float64 {
	switch kind {
	case KindCaps:
		return b.capsCeil
	case KindSymbol:
		return b.symCeil
	default:
		return b.tokCeil
	}
}

// evictStalestHalf drops the oldest half of m by lastSeen when a shard map
// hits its cap. Runs only on the overflow path (once per ~2048 new channels),
// so its sort allocation never touches the steady state.
func evictStalestHalf[V any](m map[uint64]V, lastSeen func(V) int64) {
	if len(m) < baselineChanCap {
		return
	}
	type ageKey struct {
		id   uint64
		seen int64
	}
	ages := make([]ageKey, 0, len(m))
	for id, v := range m {
		ages = append(ages, ageKey{id, lastSeen(v)})
	}
	sort.Slice(ages, func(i, j int) bool {
		if ages[i].seen != ages[j].seen {
			return ages[i].seen < ages[j].seen
		}
		return ages[i].id < ages[j].id // tie-break keeps eviction deterministic
	})
	for _, a := range ages[:len(ages)/2] {
		delete(m, a.id)
	}
}
