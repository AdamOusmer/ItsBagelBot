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
	// not a working limit. GLOBAL across all shards - eviction call sites pass
	// baselineChanCap/baselineShards per shard. Overflow drops the stalest
	// half by lastSeen so a returning active channel re-warms rather than
	// staying evicted.
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
		evictStalestHalf(s.m, baselineChanCap/baselineShards, func(cs *chanStats) int64 { return cs.lastSeen })
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

// Adjust returns the EFFECTIVE threshold for kind on channel ch. ratio is
// the profile-resolved static threshold the caller was about to apply, and it
// is the floor on every path: a cold channel sees it verbatim, so wiring a
// baseline in cannot flip a single pre-existing verdict until ~coldFloorN
// judged lines accumulate. A warm channel's learned value (mean +
// zScore·stddev) applies only when it clears BOTH ratio and the fleet
// ceiling: the ceiling clamps the LEARNED contribution alone - learned data
// is attacker-observable (frog-boiling), a broadcaster's static config is
// not, so the ceiling must never override a deliberately tighter static
// threshold (2026-08-30: it used to, silently erasing LevelStrict's 0.6 caps
// on every baseline-wired channel). Direction stays raise-only by
// construction.
func (b *Baseline) Adjust(ch uint64, kind StyleKind, ratio float64) float64 {
	t, warm := b.rawThresh(ch, kind)
	if !warm || t < b.ceilingOf(kind) {
		return ratio
	}
	return math.Max(t, ratio)
}

// rawThresh is the stats-lookup half of Adjust: kind's observed
// mean+zScore*stddev for channel ch under the shard lock, and whether the
// channel is warm. Below coldFloorN observations the variance estimate is
// garbage, so the cold path reports no learned value at all.
func (b *Baseline) rawThresh(ch uint64, kind StyleKind) (float64, bool) {
	s := &b.shards[ch&baselineShardMask]
	s.Lock()
	defer s.Unlock()
	cs := s.m[ch]
	if cs == nil || cs.n < coldFloorN {
		return 0, false
	}
	return kind.series(cs).thresh(), true
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

// ageKey pairs a channel id with its lastSeen stamp for the overflow eviction.
type ageKey struct {
	id   uint64
	seen int64
}

// olderAge is the eviction order: stalest first, ties broken on id. Channel ids
// are unique within a shard map, so this is a TOTAL order - that is what makes
// eviction deterministic, and it is why the quickselect below drops exactly the
// same half the old full sort did.
func olderAge(x, y ageKey) bool {
	if x.seen != y.seen {
		return x.seen < y.seen
	}
	return x.id < y.id
}

// evictStalestHalf drops the oldest half of m by lastSeen when a shard map
// hits maxKeys. Runs only on the overflow path, so its allocation never
// touches the steady state. maxKeys is the PER-SHARD share of the documented
// global cap - callers pass cap/shards (2026-08-30: this used to hardcode
// baselineChanCap per shard, silently making the real backstop 64x the
// documented 4096 and leaving vocabChanCap decorative).
//
// Selection, not sorting: only the MEDIAN matters, so quickselect partitions in
// O(n) expected where sort.Slice paid O(n log n) plus a closure call and a
// reflect-based swap per comparison. The ages slice itself stays - enumerating
// a Go map needs somewhere to put the keys - so this removes the log factor,
// not the O(n) allocation.
func evictStalestHalf[V any](m map[uint64]V, maxKeys int, lastSeen func(V) int64) {
	if len(m) < maxKeys {
		return
	}
	ages := make([]ageKey, 0, len(m))
	for id, v := range m {
		ages = append(ages, ageKey{id, lastSeen(v)})
	}
	half := len(ages) / 2
	selectStalest(ages, half)
	for _, a := range ages[:half] {
		delete(m, a.id)
	}
}

// selectStalest partitions ages in place so ages[:k] holds exactly the k
// stalest entries under olderAge (in arbitrary order among themselves, which is
// all the caller needs: it deletes all of them).
func selectStalest(ages []ageKey, k int) {
	lo, hi := 0, len(ages)-1
	for lo < hi {
		p := partitionAges(ages, lo, hi)
		switch {
		case p == k:
			return
		case p > k:
			hi = p - 1
		default:
			lo = p + 1
		}
	}
}

// partitionAges is Lomuto partitioning around a median-of-three pivot. The
// median-of-three is not decoration: shard maps fill in arrival order, so
// lastSeen is very often ALREADY ascending, which is precisely the input that
// makes a first/last-element pivot degrade to O(n^2). Sampling three positions
// costs three compares and removes that case without a random source (eviction
// must stay deterministic, so rand is not an option here).
func partitionAges(a []ageKey, lo, hi int) int {
	mid := lo + (hi-lo)/2
	orderAgePair(a, mid, lo)
	orderAgePair(a, hi, lo)
	orderAgePair(a, hi, mid)
	pivot := a[mid]
	a[mid], a[hi] = a[hi], a[mid]
	i := lo
	for j := lo; j < hi; j++ {
		if olderAge(a[j], pivot) {
			a[i], a[j] = a[j], a[i]
			i++
		}
	}
	a[i], a[hi] = a[hi], a[i]
	return i
}

// orderAgePair swaps a[x] and a[y] when a[x] sorts before a[y], so a[y] ends up
// holding the older of the two. Split out to keep partitionAges flat.
func orderAgePair(a []ageKey, x, y int) {
	if olderAge(a[x], a[y]) {
		a[x], a[y] = a[y], a[x]
	}
}
