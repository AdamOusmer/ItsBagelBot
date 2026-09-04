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

func TestBaselineColdChannelReturnsCallerStatic(t *testing.T) {
	b := newTestBaseline()
	// Cold channels see the caller's static threshold VERBATIM - including a
	// static tighter than the fleet ceiling (LevelStrict caps 0.6): the
	// ceiling clamps only the learned contribution, never a config choice.
	for kind, want := range map[StyleKind]float64{
		KindCaps:   0.7,
		KindSymbol: 0.6,
	} {
		if got := b.Adjust(42, kind, want); got != want {
			t.Fatalf("cold channel kind=%d: got %v, want caller static %v", kind, got, want)
		}
	}
	if got := b.Adjust(42, KindCaps, 0.6); got != 0.6 {
		t.Fatalf("cold strict caps: got %v, want the tighter static 0.6", got)
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

func TestBaselineNeverDropsBelowCallerStatic(t *testing.T) {
	b := newTestBaseline()
	// A quiet channel: the learned value sits under the fleet ceiling, so it
	// is discarded and every kind pins to the caller's static exactly - the
	// frog-boiling clamp: attacker-fed quiet lines can never tighten (or
	// loosen) the gate below what the config says.
	for i := 0; i < 500; i++ {
		b.Observe(9, 0.2, 0.1, 8)
	}
	for kind, static := range map[StyleKind]float64{KindCaps: 0.7, KindSymbol: 0.6} {
		got := b.Adjust(9, kind, static)
		if got != static {
			t.Fatalf("kind=%d quiet warm channel must pin to static exactly, got %v want %v", kind, got, static)
		}
	}
	// A learned value between a strict static (0.6) and the fleet ceiling
	// (0.7) is likewise discarded: raises act only from the ceiling up.
	for i := 0; i < 500; i++ {
		b.Observe(11, 0.62, 0.1, 8)
	}
	if got := b.Adjust(11, KindCaps, 0.6); got != 0.6 {
		t.Fatalf("sub-ceiling learned value must not move a strict static: got %v want 0.6", got)
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
	// The documented global backstop divides across shards: one shard holds
	// at most its share, so 64 shards aggregate to baselineChanCap, not 64x it.
	if len(s.m) > baselineChanCap/baselineShards {
		t.Fatalf("shard map exceeded its per-shard cap: %d > %d", len(s.m), baselineChanCap/baselineShards)
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
