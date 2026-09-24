// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"math"
	"strings"
	"sync"
	"time"
)

const (
	vocabBins = 512
	vocabTau  = 20

	vocabSenders = 8

	vocabChanCap   = 4096
	vocabMinCount  = 1.0
	vocabShardMask = baselineShards - 1
)

type Vocab struct {
	shards [baselineShards]struct {
		sync.Mutex
		m map[uint64]*chanVocab
	}
	nowUnix func() int64
}

type chanVocab struct {
	bins     map[string]*tokenStat
	lastSeen int64
}

type tokenStat struct {
	count         float64
	lastTouchHour int64
	senders       map[string]struct{}
}

func NewVocab() *Vocab {
	v := &Vocab{nowUnix: func() int64 { return time.Now().Unix() }}
	for i := range v.shards {
		v.shards[i].m = make(map[uint64]*chanVocab)
	}
	return v
}

func (v *Vocab) Observe(channel uint64, senderID string, tokens []string) {
	hour := v.nowUnix() / 3600
	s := &v.shards[channel&vocabShardMask]
	s.Lock()
	cv := vocabChannel(s.m, channel)
	cv.lastSeen = v.nowUnix()
	for _, tok := range tokens {
		recordToken(cv, hour, tokenObs{lower: strings.ToLower(tok), sender: senderID})
	}
	s.Unlock()
}

func vocabChannel(m map[uint64]*chanVocab, channel uint64) *chanVocab {
	if cv := m[channel]; cv != nil {
		return cv
	}
	evictStalestHalf(m, vocabChanCap/baselineShards, func(cv *chanVocab) int64 { return cv.lastSeen })
	cv := &chanVocab{bins: make(map[string]*tokenStat)}
	m[channel] = cv
	return cv
}

type tokenObs struct {
	lower  string
	sender string
}

func recordToken(cv *chanVocab, hour int64, obs tokenObs) {
	if obs.lower == "" {
		return
	}
	ts := cv.bins[obs.lower]
	if ts == nil {
		admitNewBin(cv.bins, hour, obs)
		return
	}
	ts.count = ts.aged(hour) + 1
	promoteSender(ts, obs.sender)
}

func admitNewBin(bins map[string]*tokenStat, hour int64, obs tokenObs) {
	if len(bins) >= vocabBins && !mgMakeRoom(bins, hour) {
		return
	}
	ts := &tokenStat{count: 1, lastTouchHour: hour}
	bins[obs.lower] = ts
	promoteSender(ts, obs.sender)
}

func promoteSender(ts *tokenStat, senderID string) {
	if senderID == "" || len(ts.senders) >= vocabSenders {
		return
	}
	if ts.senders == nil {
		ts.senders = make(map[string]struct{})
	}
	ts.senders[senderID] = struct{}{}
}

func (v *Vocab) Known(channel uint64, token string) bool {
	hour := v.nowUnix() / 3600
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

func (t *tokenStat) aged(nowHour int64) float64 {
	if t.lastTouchHour != nowHour {
		t.count *= math.Exp2(-float64(nowHour - t.lastTouchHour))
		t.lastTouchHour = nowHour
	}
	return t.count
}

func mgMakeRoom(bins map[string]*tokenStat, hour int64) bool {
	mgEvictAged(bins, hour)
	if len(bins) < vocabBins {
		return true
	}
	for _, ts := range bins {
		ts.count--
	}
	mgEvictDead(bins)
	return len(bins) < vocabBins
}

func mgEvictAged(bins map[string]*tokenStat, hour int64) {
	for tok, ts := range bins {
		if ts.aged(hour) < vocabMinCount {
			delete(bins, tok)
		}
	}
}

func mgEvictDead(bins map[string]*tokenStat) {
	for tok, ts := range bins {
		if ts.count < vocabMinCount {
			delete(bins, tok)
		}
	}
}
