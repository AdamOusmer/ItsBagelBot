// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"sync"
	"testing"
)

func newTestBaseline() *Baseline {
	b := NewBaseline(DefaultCeiling())
	b.now = func() int64 { return 1_800_000_000 }
	return b
}

func TestBaselineColdChannelReturnsPlainCeiling(t *testing.T) {
	b := newTestBaseline()
	for kind, want := range map[StyleKind]float64{
		KindCaps:   0.7,
		KindSymbol: 0.6,
	} {
		if got := b.Adjust(42, kind, 0.0); got != want {
			t.Fatalf("cold channel kind=%d: got %v, want plain ceiling %v", kind, got, want)
		}
	}
}

func TestBaselineWarmChannelRaisesForHypeCulture(t *testing.T) {
	b := newTestBaseline()
	// A channel whose judged lines alternate 0.50/0.70 caps: mean 0.60,
	// stddev 0.10, so mean+2sigma = 0.80 tops the fleet ceiling and shouting
	// at 0.72 stops striking.
	for i := 0; i < 200; i++ {
		v := 0.55
		if i%2 == 0 {
			v = 0.75
		}
		b.Observe(7, v-0.05, 0.1, 10)
	}
	if got := b.Adjust(7, KindCaps, 0.7); got <= 0.7 {
		t.Fatalf("hype channel must raise above ceiling, got %v", got)
	}
}

func TestBaselineNeverDropsBelowCeiling(t *testing.T) {
	b := newTestBaseline()
	// A quiet channel: every branch of Adjust bottoms out at the ceiling.
	for i := 0; i < 500; i++ {
		b.Observe(9, 0.2, 0.1, 8)
	}
	for kind, floor := range map[StyleKind]float64{KindCaps: 0.7, KindSymbol: 0.6} {
		got := b.Adjust(9, kind, 0.0)
		if got != floor {
			t.Fatalf("kind=%d quiet warm channel must pin to ceiling exactly, got %v want %v", kind, got, floor)
		}
	}
}

func TestBaselineRespectsCallerStaticThreshold(t *testing.T) {
	b := newTestBaseline()
	for i := 0; i < 200; i++ {
		b.Observe(3, 0.5, 0.1, 10)
	}
	if got := b.Adjust(3, KindCaps, 0.85); got != 0.85 {
		t.Fatalf("stricter caller threshold must survive adaptation: got %v want 0.85", got)
	}
}

func TestBaselineEvictsStalestHalfAtCap(t *testing.T) {
	b := newTestBaseline()
	const shardBase = uint64(1 << 10) // low bits zero => same shard
	for i := uint64(0); i < baselineChanCap+512; i++ {
		ch := shardBase + i*64
		b.now = func() int64 { return int64(1_800_000_000 + i) }
		b.Observe(ch, 0.3, 0.1, 5)
	}
	s := &b.shards[shardBase&baselineShardMask]
	if len(s.m) > baselineChanCap {
		t.Fatalf("shard map exceeded cap: %d", len(s.m))
	}
	freshest := shardBase + (baselineChanCap+511)*64
	if _, ok := s.m[freshest]; !ok {
		t.Fatal("the most recently seen channel was evicted; eviction must keep the hot half")
	}
}

func TestBaselineShardsIsolateAndRace(t *testing.T) {
	b := newTestBaseline()
	a, c := uint64(100), uint64(101)
	if (&b.shards[a&baselineShardMask]) == (&b.shards[c&baselineShardMask]) {
		t.Fatal("adjacent ids must not share a shard or they contend on one lock")
	}
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ch := uint64(g)*64 + 7 // distinct shards per goroutine...
			for i := 0; i < 500; i++ {
				b.Observe(ch, 0.4, 0.2, float64(i))
				b.Adjust(ch, KindCaps, 0.7)
			}
			for i := 0; i < 200; i++ { // ...plus one shared hot channel
				b.Observe(uint64(4096), 0.4, 0.2, 12)
				b.Adjust(uint64(4096), KindSymbol, 0.6)
			}
		}(g)
	}
	wg.Wait()
}

func TestBaselineZeroAllocSteadyState(t *testing.T) {
	b := newTestBaseline()
	ch := uint64(55)
	for i := 0; i < 100; i++ {
		b.Observe(ch, 0.5, 0.3, 11)
	}
	b.Adjust(ch, KindCaps, 0.7)
	if got := testing.AllocsPerRun(1000, func() {
		b.Observe(ch, 0.5, 0.3, 11)
	}); got != 0 {
		t.Fatalf("Observe steady state allocates %v times/run", got)
	}
	if got := testing.AllocsPerRun(1000, func() {
		b.Adjust(ch, KindSymbol, 0.6)
	}); got != 0 {
		t.Fatalf("Adjust steady state allocates %v times/run", got)
	}
}
