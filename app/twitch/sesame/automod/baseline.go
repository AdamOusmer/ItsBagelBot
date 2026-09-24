// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"math"
	"sync"
	"time"
)

const (
	baselineShards    = 64
	baselineShardMask = baselineShards - 1

	ewmaAlpha  = 0.05
	zScore     = 2.0
	coldFloorN = 50

	baselineChanCap = 4096
)

type StyleKind uint8

const (
	KindCaps StyleKind = iota
	KindSymbol
	KindTokenLen
)

type Ceiling struct {
	Caps     float64
	Symbol   float64
	TokenLen float64
}

func DefaultCeiling() Ceiling { return Ceiling{Caps: capsThreshold, Symbol: symbolRatioHi} }

type Baseline struct {
	shards [baselineShards]struct {
		sync.Mutex
		m map[uint64]*chanStats
	}
	capsCeil, symCeil, tokCeil float64
	nowUnix                    func() int64
}

type chanStats struct {
	n              uint32
	lastSeen       int64
	caps, sym, tok metricEWMA
}

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
		nowUnix:  func() int64 { return time.Now().Unix() },
	}
	for i := range b.shards {
		b.shards[i].m = make(map[uint64]*chanStats)
	}
	return b
}

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
	cs.lastSeen = b.nowUnix()
	cs.caps.add(caps)
	cs.sym.add(symbol)
	cs.tok.add(tokens)
	s.Unlock()
}

func (b *Baseline) Adjust(ch uint64, kind StyleKind, ratio float64) float64 {
	t, warm := b.rawThresh(ch, kind)
	if !warm || t < b.ceilingOf(kind) {
		return ratio
	}
	return math.Max(t, ratio)
}

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

type ageKey struct {
	id   uint64
	seen int64
}

func olderAge(x, y ageKey) bool {
	if x.seen != y.seen {
		return x.seen < y.seen
	}
	return x.id < y.id
}

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

func orderAgePair(a []ageKey, x, y int) {
	if olderAge(a[x], a[y]) {
		a[x], a[y] = a[y], a[x]
	}
}
