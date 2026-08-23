// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"fmt"
	"math"
	"sync"
	"testing"
)

func newTestVocab() *Vocab {
	v := NewVocab()
	v.now = func() int64 { return 1_800_000_000 }
	return v
}

// learnPattern feeds the tau x d pattern: vocabSenders senders, each saying the
// token enough times that total uses clear vocabTau within one hour (no decay).
func learnPattern(t *testing.T, v *Vocab, token string) {
	t.Helper()
	for s := 0; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(1, fmt.Sprintf("user-%d", s), []string{token})
		}
	}
}

func TestVocabLearnsAfterTauByDPattern(t *testing.T) {
	v := newTestVocab()
	learnPattern(t, v, "poggers")
	if !v.Known(1, "POGGERS") {
		t.Fatal("tau x d consensus must learn the token (lookup is case-insensitive)")
	}
	if v.Known(1, "neverseen") {
		t.Fatal("unknown token reported Known")
	}
}

func TestVocabSingleSenderFloodNeverLearns(t *testing.T) {
	v := newTestVocab()
	for i := 0; i < 1000; i++ { // 50x tau from ONE account: laundering shape
		v.Observe(1, "launderer", []string{"freediscord"})
	}
	ts := v.shards[1&vocabShardMask].m[1].bins["freediscord"]
	if got := len(ts.senders); got != 1 {
		t.Fatalf("single sender recorded %d distinct senders", got)
	}
	if v.Known(1, "freediscord") {
		t.Fatal("d-sender consensus defeated: single-account flood learned a token")
	}
}

func TestVocabSenderSetCapsAtD(t *testing.T) {
	v := newTestVocab()
	for s := 0; s < vocabSenders*4; s++ {
		v.Observe(1, fmt.Sprintf("user-%d", s), []string{"busytoken"})
	}
	ts := v.shards[1&vocabShardMask].m[1].bins["busytoken"]
	if len(ts.senders) != vocabSenders {
		t.Fatalf("sender set grew past d: %d", len(ts.senders))
	}
}

func TestVocabDecayHalvesHourly(t *testing.T) {
	v := newTestVocab()
	hour := int64(1_800_000_000 / 3600)
	v.now = func() int64 { return hour * 3600 }
	for i := 0; i < 4; i++ {
		v.Observe(1, "u", []string{"fading"})
	}
	ts := v.shards[1&vocabShardMask].m[1].bins["fading"]
	hour++
	if got := ts.aged(hour); math.Abs(got-2.0) > 1e-9 {
		t.Fatalf("one silent hour must halve 4 -> 2, got %v", got)
	}
	hour += 2 // two more hours: 2 -> 0.5, under the floor
	if got := ts.aged(hour); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("three silent hours must take 4 -> 0.5, got %v", got)
	}
	if v.Known(1, "fading") {
		t.Fatal("a decayed husk must not read as Known")
	}
}

func TestVocabPurgeTokensRemoves(t *testing.T) {
	v := newTestVocab()
	learnPattern(t, v, "edgecase")
	if !v.Known(1, "edgecase") {
		t.Fatal("setup failed: token should be learned before purge")
	}
	v.PurgeTokens(1, []string{"EdgeCase"})
	if v.Known(1, "edgecase") {
		t.Fatal("purged token still Known; whitewash reset failed")
	}
	// Purge on an unknown channel must not panic or create state.
	v.PurgeTokens(999, []string{"ghost"})
	if _, ok := v.shards[999&vocabShardMask].m[999]; ok {
		t.Fatal("purge must not mint channel rows")
	}
}

func TestVocabMisraGriesWindowBounded(t *testing.T) {
	v := newTestVocab()
	misraGriesChurnStorm(v)
	topUpHeavyHitterSenders(v)
	cv := v.shards[2&vocabShardMask].m[2]
	if len(cv.bins) > vocabBins {
		t.Fatalf("MG window exceeded K: %d bins", len(cv.bins))
	}
	if !v.Known(2, "heavyhitter") {
		t.Fatal("a true heavy hitter was evicted by one-off churn")
	}
}

// misraGriesChurnStorm interleaves ~40 heavy-hitter uses into 3K one-off churn
// on channel 2: every one-off insert decrements all bins once, so only genuinely
// frequent tokens survive (MG guarantee: anything above ~N/(K+1) frequency
// stays).
func misraGriesChurnStorm(v *Vocab) {
	const churn = vocabBins * 3
	for i := 0; i < churn; i++ {
		v.Observe(2, fmt.Sprintf("u%d", i%vocabSenders), []string{fmt.Sprintf("tok%05d", i)})
		if i%(churn/(vocabTau*2)) == 0 { // ~40 uses spread through the storm...
			v.Observe(2, "heavy0", []string{"heavyhitter"})
		}
	}
}

// topUpHeavyHitterSenders tops the heavy hitter up with the remaining senders
// and uses past tau x d (misraGriesChurnStorm seeded "heavy0" only).
func topUpHeavyHitterSenders(v *Vocab) {
	for s := 1; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(2, fmt.Sprintf("heavy%d", s), []string{"heavyhitter"})
		}
	}
}

func TestVocabZeroAllocSteadyState(t *testing.T) {
	v := newTestVocab()
	ch := uint64(3)
	// Pre-warm every allocation site: row, bin, full sender set.
	for s := 0; s < vocabSenders; s++ {
		for i := 0; i < vocabTau/vocabSenders+1; i++ {
			v.Observe(ch, fmt.Sprintf("u%d", s), []string{"warm"})
		}
	}
	got := testing.AllocsPerRun(1000, func() {
		v.Observe(ch, "u0", []string{"warm"}) // existing bin, sender set already at d
	})
	if got != 0 {
		t.Fatalf("Observe steady state allocates %v times/run", got)
	}
	knows := false
	got = testing.AllocsPerRun(1000, func() {
		knows = v.Known(ch, "warm")
	})
	if got != 0 || !knows {
		t.Fatalf("Known allocates %v times/run or lost the token (%v)", got, knows)
	}
}

func TestVocabConcurrentShardsRace(t *testing.T) {
	v := newTestVocab()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ch := uint64(g)*64 + 13
			for i := 0; i < 300; i++ {
				v.Observe(ch, fmt.Sprintf("u%d", g), []string{fmt.Sprintf("t%d", i%10)})
				v.Known(ch, "t1")
			}
			for i := 0; i < 100; i++ { // shared hot channel exercises lock contention
				v.Observe(uint64(8192), fmt.Sprintf("shared%d", g%vocabSenders), []string{"hot"})
				v.PurgeTokens(uint64(8192), []string{"cold"})
			}
		}(g)
	}
	wg.Wait()
}
