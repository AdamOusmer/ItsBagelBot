// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func observeConstant(b *Baseline, ch uint64, n int, caps float64) {
	for i := 0; i < n; i++ {
		b.Observe(ch, caps, 0.1, 8)
	}
}

func observeHypeAlternation(b *Baseline, ch uint64, n int) {
	for i := 0; i < n; i++ {
		caps := 0.5
		if i%2 == 0 {
			caps = 0.8
		}
		b.Observe(ch, caps, 0.1, 10)
	}
}

func TestBaselineAdjust(t *testing.T) {
	const ch = uint64(7)
	tests := []struct {
		name   string
		warm   func(*Baseline)
		kind   StyleKind
		static float64
		raised bool
	}{
		{"returns a stricter caller caps static for a cold channel", func(*Baseline) {}, KindCaps, 0.85, false},
		{"returns a stricter caller symbol static for a cold channel", func(*Baseline) {}, KindSymbol, 0.9, false},
		{"raises caps above the static for a warm hype channel", func(b *Baseline) { observeHypeAlternation(b, ch, 200) }, KindCaps, 0.7, true},
		{"keeps a stricter caller threshold through adaptation", func(b *Baseline) { observeConstant(b, ch, 200, 0.5) }, KindCaps, 0.85, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newTestBaseline()
			tt.warm(b)

			got := b.Adjust(ch, tt.kind, tt.static)

			if tt.raised {
				assert.Greater(t, got, tt.static)
				return
			}
			assert.Equal(t, tt.static, got)
		})
	}
}

func TestBaselineColdChannelReturnsCallerStatic(t *testing.T) {
	b := newTestBaseline()
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

func TestBaselineNeverDropsBelowCallerStatic(t *testing.T) {
	b := newTestBaseline()
	for i := 0; i < 500; i++ {
		b.Observe(9, 0.2, 0.1, 8)
	}
	for kind, static := range map[StyleKind]float64{KindCaps: 0.7, KindSymbol: 0.6} {
		got := b.Adjust(9, kind, static)
		if got != static {
			t.Fatalf("kind=%d quiet warm channel must pin to static exactly, got %v want %v", kind, got, static)
		}
	}
	for i := 0; i < 500; i++ {
		b.Observe(11, 0.62, 0.1, 8)
	}
	if got := b.Adjust(11, KindCaps, 0.6); got != 0.6 {
		t.Fatalf("sub-ceiling learned value must not move a strict static: got %v want 0.6", got)
	}
}

func TestBaselineEvictsStalestHalfAtCap(t *testing.T) {
	b := newTestBaseline()
	const shardBase = uint64(1 << 10)
	for i := uint64(0); i < baselineChanCap+512; i++ {
		b.nowUnix = func() int64 { return int64(1_800_000_000 + i) }
		b.Observe(shardBase+i*64, 0.3, 0.1, 5)
	}

	s := &b.shards[shardBase&baselineShardMask]

	assert.LessOrEqual(t, len(s.m), baselineChanCap/baselineShards, "shard map exceeded its per-shard cap")
	assert.Contains(t, s.m, shardBase+(baselineChanCap+511)*64, "eviction must keep the hot half")
}

func TestBaselineShardsIsolateAndRace(t *testing.T) {
	b := newTestBaseline()
	assert.NotSame(t, &b.shards[uint64(100)&baselineShardMask], &b.shards[uint64(101)&baselineShardMask],
		"adjacent ids must not share a shard or they contend on one lock")

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ch := uint64(g)*64 + 7
			for i := 0; i < 500; i++ {
				b.Observe(ch, 0.4, 0.2, float64(i))
				b.Adjust(ch, KindCaps, 0.7)
			}
			for i := 0; i < 200; i++ {
				b.Observe(4096, 0.4, 0.2, 12)
				b.Adjust(4096, KindSymbol, 0.6)
			}
		}(g)
	}
	wg.Wait()
}

func TestBaselineSteadyStateAllocatesNothing(t *testing.T) {
	b := newTestBaseline()
	const ch = uint64(55)
	for i := 0; i < 100; i++ {
		b.Observe(ch, 0.5, 0.3, 11)
	}

	assert.Zero(t, testing.AllocsPerRun(1000, func() { b.Observe(ch, 0.5, 0.3, 11) }), "Observe")
	assert.Zero(t, testing.AllocsPerRun(1000, func() { b.Adjust(ch, KindSymbol, 0.6) }), "Adjust")
}
