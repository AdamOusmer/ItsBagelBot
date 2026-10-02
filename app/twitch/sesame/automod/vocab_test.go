// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func learnPattern(v *Vocab, ch uint64, token string) {
	for s := 0; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(ch, fmt.Sprintf("user-%d", s), []string{token})
		}
	}
}

func TestVocabKnown(t *testing.T) {
	tests := []struct {
		name    string
		observe func(*Vocab)
		channel uint64
		token   string
		want    bool
	}{
		{"learns a token after tau observations from d senders", func(v *Vocab) { learnPattern(v, 1, "poggers") }, 1, "poggers", true},
		{"looks tokens up case-insensitively", func(v *Vocab) { learnPattern(v, 1, "poggers") }, 1, "POGGERS", true},
		{"reports an unseen token as unknown", func(v *Vocab) { learnPattern(v, 1, "poggers") }, 1, "neverseen", false},
		{"reports a token on an unseen channel as unknown", func(v *Vocab) { learnPattern(v, 1, "poggers") }, 99, "poggers", false},
		{
			"never learns from a single sender flood",
			func(v *Vocab) {
				for i := 0; i < 1000; i++ {
					v.Observe(1, "launderer", []string{"freediscord"})
				}
			},
			1, "freediscord", false,
		},
		{
			"keeps a heavy hitter through one-off churn",
			func(v *Vocab) {
				misraGriesChurnStorm(v)
				topUpHeavyHitterSenders(v)
			},
			2, "heavyhitter", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestVocab()
			tt.observe(v)

			assert.Equal(t, tt.want, v.Known(tt.channel, tt.token))
		})
	}
}

func TestVocabForgetsAfterASilentHour(t *testing.T) {
	v := newTestVocab()
	learnPattern(v, 1, "fading")
	assert.True(t, v.Known(1, "fading"))

	now := v.nowUnix()
	v.nowUnix = func() int64 { return now + 3600 }

	assert.False(t, v.Known(1, "fading"), "a decayed husk must not read as known")
}

func TestVocabPurgeTokensRemovesAndMintsNothing(t *testing.T) {
	v := newTestVocab()
	learnPattern(v, 1, "edgecase")
	assert.True(t, v.Known(1, "edgecase"))

	v.PurgeTokens(1, []string{"EdgeCase"})
	v.PurgeTokens(999, []string{"ghost"})

	assert.False(t, v.Known(1, "edgecase"), "a purged token must reset")
	assert.NotContains(t, v.shards[999&vocabShardMask].m, uint64(999), "purge must not mint channel rows")
}

func TestVocabBoundsItsMemory(t *testing.T) {
	v := newTestVocab()
	for s := 0; s < vocabSenders*4; s++ {
		v.Observe(1, fmt.Sprintf("user-%d", s), []string{"busytoken"})
	}
	misraGriesChurnStorm(v)

	assert.Len(t, v.shards[1&vocabShardMask].m[1].bins["busytoken"].senders, vocabSenders, "sender set must stop at d")
	assert.LessOrEqual(t, len(v.shards[2&vocabShardMask].m[2].bins), vocabBins, "the Misra-Gries window must stay within K")
}

func misraGriesChurnStorm(v *Vocab) {
	const churn = vocabBins * 3
	for i := 0; i < churn; i++ {
		v.Observe(2, fmt.Sprintf("u%d", i%vocabSenders), []string{fmt.Sprintf("tok%05d", i)})
		if i%(churn/(vocabTau*2)) == 0 {
			v.Observe(2, "heavy0", []string{"heavyhitter"})
		}
	}
}

func topUpHeavyHitterSenders(v *Vocab) {
	for s := 1; s < vocabSenders; s++ {
		for u := 0; u < vocabTau/vocabSenders+1; u++ {
			v.Observe(2, fmt.Sprintf("heavy%d", s), []string{"heavyhitter"})
		}
	}
}

func TestVocabSteadyStateAllocatesNothing(t *testing.T) {
	v := newTestVocab()
	learnPattern(v, 3, "warm")

	assert.Zero(t, testing.AllocsPerRun(1000, func() { v.Observe(3, "u0", []string{"warm"}) }), "Observe")
	assert.Zero(t, testing.AllocsPerRun(1000, func() { _ = v.Known(3, "warm") }), "Known")
	assert.True(t, v.Known(3, "warm"))
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
			for i := 0; i < 100; i++ {
				v.Observe(8192, fmt.Sprintf("shared%d", g%vocabSenders), []string{"hot"})
				v.PurgeTokens(8192, []string{"cold"})
			}
		}(g)
	}
	wg.Wait()
}
