// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
//
// Learned community vocabulary per channel, on a Misra-Gries top-K counter
// with lazy d-sender promotion and hourly half-life decay. A token becomes
// KNOWN for a channel only after tau uses from d distinct senders; the gate
// may then treat it as communal (emote-speak, in-jokes) instead of striking
// it.
//
// Wiring (since 2026-08-23): main.go installs the store via Gate.SetExtraEmotes
// - Vocab.Known's exact shape IS the ExtraEmotes seam, no adapter - which makes
// it layer (c) of emoteDominant membership AND enables two more behaviors in
// learned.go:
//
//   - gate.Assess feeds Vocab.Observe(ch, senderID, tokens) for every judged
//     line's whitespace tokens before any verdict resolves;
//   - when caps/symbol already flagged, stripLearned subtracts the letters/
//     symbols contributed by Known tokens and re-runs those comparisons -
//     style evidence only: zeroWidth/repeat evasion signals are never shed;
//   - on any lexicon/hate-floor/block-term verdict, purgeLearned calls
//     PurgeTokens(ch, messageTokens) so laundering cannot ride a token
//     through the learner and then attach slurs to a Known word.
//
// Attack model, for the reviewer:
//   - d-sender consensus: one account flooding a slur-adjacent string never
//     reaches d distinct senders, so single-account laundering cannot mint a
//     Known token no matter how long it floods.
//   - purge-on-strike: a token that ever draws a lexicon/floor strike is
//     removed with its sender set, so "get it learned, then attach slurs"
//     resets to zero on first enforcement contact.
//   - Residual, accepted: coordinated slow-launder across >= d accounts that
//     never trips a strike can still learn tokens over hours of real chat.
//     Defeating that requires cross-channel reputation graphing (see
//     engine/council campaign work); within this file's ~100-line budget the
//     mitigation is decay + purge, consistent with the literature on
//     vocabulary poisoning in adaptive filters (e.g. Lowd & Meek 2005,
//     adversarial learning; Nelson et al. 2009 on evasion attacks).
package automod

import (
	"math"
	"strings"
	"sync"
	"time"
)

const (
	// vocabBins is the Misra-Gries window K=512: at Twitch chat volume the
	// true heavy hitters of a channel number in the dozens; 512 leaves 10x
	// headroom while keeping worst-case memory per channel bounded and small
	// (see the byte math at the bottom of this file).
	vocabBins = 512

	// vocabTau 20 uses before a token is even promotable — below this, noise
	// dominates: the audit's FP corpus was dominated by one-off reaction spam
	// that would each clear a low bar instantly.
	vocabTau = 20

	// vocabSenders d=8 distinct senders required alongside vocabTau. Chosen so
	// laundering needs 8 colluding accounts per token per channel; smaller d
	// was trivially brigaded in the audit's raid-shaped traffic.
	vocabSenders = 8

	// vocabChanCap matches bot_stats.go channelStatsMaxKeys discipline (see
	// baselineChanCap): stalest-half eviction on overflow.
	vocabChanCap = 4096

	// vocabMinCount floor-evicts entries whose decaying count drops under 1.0:
	// an hour of silence halves a count of 2 into oblivion, so dead channels'
	// vocabularies self-clean without a sweeper goroutine.
	vocabMinCount = 1.0

	// vocabShardMask mirrors baseline's shard geometry.
	vocabShardMask = baselineShards - 1
)

// Vocab learns which lowercase tokens a channel's community uses heavily and
// from how many distinct voices. Build once at module wiring; all methods are
// goroutine-safe.
type Vocab struct {
	shards [baselineShards]struct {
		sync.Mutex
		m map[uint64]*chanVocab
	}
	now func() int64 // unix seconds; injectable for deterministic tests
}

type chanVocab struct {
	bins     map[string]*tokenStat
	lastSeen int64 // unix seconds, drives channel-level stalest-half eviction
}

type tokenStat struct {
	count     float64
	lastTouch int64 // unix HOUR, drives lazy half-life decay
	senders   map[string]struct{}
}

// NewVocab builds an empty Vocab.
func NewVocab() *Vocab {
	v := &Vocab{now: func() int64 { return time.Now().Unix() }}
	for i := range v.shards {
		v.shards[i].m = make(map[uint64]*chanVocab)
	}
	return v
}

// Observe folds one judged line's tokens into channel ch's counters. Tokens
// are lowercased here; senders past vocabSenders stop being recorded but keep
// counting toward tau.
func (v *Vocab) Observe(channel uint64, senderID string, tokens []string) {
	hour := v.now() / 3600
	s := &v.shards[channel&vocabShardMask]
	s.Lock()
	cv := vocabChannel(s.m, channel)
	cv.lastSeen = v.now()
	for _, tok := range tokens {
		recordToken(cv, hour, senderID, strings.ToLower(tok))
	}
	s.Unlock()
}

// vocabChannel loads channel's row, evicting the shard's stalest half when a
// new row lands in a full map (vocabChanCap backstop).
func vocabChannel(m map[uint64]*chanVocab, channel uint64) *chanVocab {
	if cv := m[channel]; cv != nil {
		return cv
	}
	evictStalestHalf(m, func(cv *chanVocab) int64 { return cv.lastSeen })
	cv := &chanVocab{bins: make(map[string]*tokenStat)}
	m[channel] = cv
	return cv
}

// recordToken folds one lowercased token use into the channel's window:
// existing bins age-decay then increment and grow their sender set; new bins
// enter through Misra-Gries admission.
func recordToken(cv *chanVocab, hour int64, senderID, tok string) {
	if tok == "" {
		return
	}
	ts := cv.bins[tok]
	if ts == nil {
		admitNewBin(cv.bins, hour, senderID, tok)
		return
	}
	ts.count = ts.aged(hour) + 1
	promoteSender(ts, senderID)
}

// admitNewBin installs a fresh bin at count 1 - or drops the token instead when
// the window is full and mgMakeRoom cannot free a bin (every surviving bin
// holds more than one use; growing would break Misra-Gries bounds).
func admitNewBin(bins map[string]*tokenStat, hour int64, senderID, tok string) {
	if len(bins) >= vocabBins && !mgMakeRoom(bins, hour) {
		return
	}
	ts := &tokenStat{count: 1, lastTouch: hour}
	bins[tok] = ts
	promoteSender(ts, senderID)
}

// promoteSender records sender among the token's distinct voices until the set
// holds vocabSenders of them. Senders past the cap keep counting toward tau but
// stop being recorded; an empty senderID (or a cohort fold without one) mints
// no diversity on its own.
func promoteSender(ts *tokenStat, senderID string) {
	if senderID == "" || len(ts.senders) >= vocabSenders {
		return
	}
	if ts.senders == nil {
		ts.senders = make(map[string]struct{})
	}
	ts.senders[senderID] = struct{}{}
}

// Known reports whether token is a LEARNED member of channel ch's vocabulary:
// tau uses AND d distinct senders, both measured after decay. Only the learned
// set counts — a merely-frequent token gets no gate leniency.
func (v *Vocab) Known(channel uint64, token string) bool {
	hour := v.now() / 3600
	token = strings.ToLower(token)
	s := &v.shards[channel&vocabShardMask]
	s.Lock()
	defer s.Unlock()
	cv := s.m[channel]
	if cv == nil {
		return false
	}
	ts := cv.bins[token]
	if ts == nil {
		return false
	}
	return ts.aged(hour) >= vocabTau && len(ts.senders) >= vocabSenders
}

// PurgeTokens forgets tokens outright, called when channel ch draws a
// lexicon/floor strike: enforcement contact whitelists nothing, so any
// progress a slur-adjacent token made toward Known dies here.
func (v *Vocab) PurgeTokens(channel uint64, tokens []string) {
	s := &v.shards[channel&vocabShardMask]
	s.Lock()
	defer s.Unlock()
	cv := s.m[channel]
	if cv == nil {
		return
	}
	for _, tok := range tokens {
		delete(cv.bins, strings.ToLower(tok))
	}
}

// aged applies the hourly half-life lazily: cost paid only across an hour
// boundary, then once per subsequent access until the next boundary. Counts
// under vocabMinCount are dead entries the caller should drop.
func (t *tokenStat) aged(nowHour int64) float64 {
	if t.lastTouch != nowHour {
		t.count *= math.Exp2(-float64(nowHour - t.lastTouch))
		t.lastTouch = nowHour
	}
	return t.count
}

// mgMakeRoom is Misra-Gries decrement-on-full: age everything (lazy decay),
// floor-evict the dead, and if the window is STILL full subtract 1 from every
// counter, dropping zeros. Returns false only if every survivor holds more
// than one use, meaning there is genuinely no room. Iteration order does not
// affect the outcome: each bin's update is independent.
func mgMakeRoom(bins map[string]*tokenStat, hour int64) bool {
	var dead []string
	sweep := func() {
		for _, tok := range dead {
			delete(bins, tok)
		}
		dead = dead[:0]
	}
	for tok, ts := range bins {
		if ts.aged(hour) < vocabMinCount {
			dead = append(dead, tok)
		}
	}
	sweep()
	if len(bins) < vocabBins {
		return true
	}
	for tok, ts := range bins {
		ts.count--
		if ts.count < vocabMinCount {
			dead = append(dead, tok)
		}
	}
	sweep()
	return len(bins) < vocabBins
}

// Memory bound, worst case per channel: vocabBins × (tokenStat struct 24B +
// ~50B map-slot overhead + ~16B token string + 8-entry sender set ≈ 350B)
// ≈ 180KB/channel — reached only when every one of 512 bins is fully promoted
// with 8 distinct senders; typical channels sit orders of magnitude lower
// because most bins hold one or two uses with 1-2 senders. Channel rows are
// capped at vocabChanCap per shard (× 64 shards), matching bot_stats.go's
// backstop-not-working-limit rationale.
