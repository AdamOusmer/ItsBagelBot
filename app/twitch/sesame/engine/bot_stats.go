// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/data"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	botStatsFlushInterval = 2 * time.Second

	counterMessagesProcessed = data.CounterMessagesProcessed
	counterEventsProcessed   = data.CounterEventsProcessed

	counterCommandsAnswered = data.CounterCommandsAnswered
	counterModActions       = data.CounterModActionsTaken

	channelStatsFlushTicks = 15

	channelStatsMaxKeys = 4096

	flagRuleSlotCap = 24
)

type flagRule string

const (
	ruleIPLogger       flagRule = "ip_logger"
	ruleScam           flagRule = "scam"
	rulePhish          flagRule = "phish"
	ruleHeuristic      flagRule = "heuristic"
	ruleBlockTerm      flagRule = "block_term"
	ruleLexHate        flagRule = "lex:hate:"
	ruleLexHarassment  flagRule = "lex:harassment:"
	ruleLexSexual      flagRule = "lex:sexual:"
	ruleLexProfanity   flagRule = "lex:profanity:"
	ruleCouncil        flagRule = "council:campaign"
	ruleSuffixRepeat   flagRule = "+repeat"
	ruleSuffixCampaign flagRule = "+campaign"
	ruleShieldMode     flagRule = "shield_mode"
	ruleOther          flagRule = "other"
)

type flagRuleBucket int

const (
	bktIPLogger flagRuleBucket = iota
	bktScam
	bktPhish
	bktHeuristic
	bktBlockTerm
	bktLexHate
	bktLexHarassment
	bktLexSexual
	bktLexProfanity
	bktCouncil
	bktShieldMode
	bktOther
	bktCount
)

var flagRuleNames = [bktCount]flagRule{
	bktIPLogger:      ruleIPLogger,
	bktScam:          ruleScam,
	bktPhish:         rulePhish,
	bktHeuristic:     ruleHeuristic,
	bktBlockTerm:     ruleBlockTerm,
	bktLexHate:       "lex_hate",
	bktLexHarassment: "lex_harassment",
	bktLexSexual:     "lex_sexual",
	bktLexProfanity:  "lex_profanity",
	bktCouncil:       "council_campaign",
	bktShieldMode:    ruleShieldMode,
	bktOther:         ruleOther,
}

func flagBucket(rule flagRule) flagRuleBucket {
	return baseRuleBucket(stripRuleSuffixes(rule))
}

func stripRuleSuffixes(rule flagRule) flagRule {
	base := rule
	for {
		if s, ok := strings.CutSuffix(string(base), string(ruleSuffixRepeat)); ok {
			base = flagRule(s)
			continue
		}
		if s, ok := strings.CutSuffix(string(base), string(ruleSuffixCampaign)); ok {
			base = flagRule(s)
			continue
		}
		break
	}
	return base
}

var baseRuleBuckets = map[flagRule]flagRuleBucket{
	ruleIPLogger:   bktIPLogger,
	ruleScam:       bktScam,
	rulePhish:      bktPhish,
	ruleHeuristic:  bktHeuristic,
	ruleBlockTerm:  bktBlockTerm,
	ruleCouncil:    bktCouncil,
	ruleShieldMode: bktShieldMode,
}

var lexBaseRules = [...]struct {
	prefix flagRule
	bucket flagRuleBucket
}{
	{ruleLexHate, bktLexHate},
	{ruleLexHarassment, bktLexHarassment},
	{ruleLexSexual, bktLexSexual},
	{ruleLexProfanity, bktLexProfanity},
}

func baseRuleBucket(base flagRule) flagRuleBucket {
	if bkt, ok := baseRuleBuckets[base]; ok {
		return bkt
	}
	for _, lx := range lexBaseRules {
		if strings.HasPrefix(string(base), string(lx.prefix)) {
			return lx.bucket
		}
	}
	return bktOther
}

const (
	flagFieldTotal    = "flags_total"
	flagFieldEnforced = "flags_enforced"
	flagFieldRulePfx  = "flag_rule_"
	flagFieldChannels = "channels"
	flagFieldChanID   = "broadcaster_id"
)

type botStats struct {
	events   atomic.Int64
	messages atomic.Int64

	flagsTotal    atomic.Int64
	flagsEnforced atomic.Int64
	flagsByRule   [flagRuleSlotCap]atomic.Int64

	mu       sync.Mutex
	channels map[uint64]*chanTally

	log    *zap.Logger
	bumper CounterBumper
	done   chan struct{}
}

type chanTally struct {
	events   int64
	messages int64
	flags    int64
	enforced int64
	answered int64
	rules    *[bktCount]int64
}

func newBotStats(bumper CounterBumper, log ...*zap.Logger) *botStats {
	l := zap.NewNop()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	s := &botStats{bumper: bumper, done: make(chan struct{}), channels: map[uint64]*chanTally{}, log: l}
	go func() {
		ticker := time.NewTicker(botStatsFlushInterval)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-ticker.C:
				ticks++
				s.flushTotals()
				if ticks%channelStatsFlushTicks == 0 {
					s.flushChannels()
				}
			case <-s.done:
				return
			}
		}
	}()
	return s
}

func (s *botStats) count(broadcasterID uint64, isChat bool) {
	if s == nil {
		return
	}
	s.events.Add(1)
	if isChat {
		s.messages.Add(1)
	}
	if broadcasterID != 0 {
		s.countChannel(broadcasterID, isChat)
	}
}

func (s *botStats) flag(broadcasterID uint64, rule flagRule, enforced bool) {
	if s == nil {
		return
	}
	b := flagBucket(rule)
	s.flagsTotal.Add(1)
	s.flagsByRule[b].Add(1)
	var enforcedDelta int64
	if enforced {
		enforcedDelta = 1
		s.flagsEnforced.Add(1)
	}
	if broadcasterID != 0 {
		s.flagChannel(broadcasterID, b, enforcedDelta)
	}
}

func (s *botStats) countChannel(broadcasterID uint64, isChat bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tally := s.channelTallyLocked(broadcasterID)
	if tally == nil {
		return
	}
	tally.events++
	if isChat {
		tally.messages++
	}
}

func (s *botStats) countAnswered(broadcasterID uint64) {
	if s == nil || broadcasterID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tally := s.channelTallyLocked(broadcasterID)
	if tally == nil {
		return
	}
	tally.answered++
}

func (s *botStats) flagChannel(broadcasterID uint64, b flagRuleBucket, enforcedDelta int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tally := s.channelTallyLocked(broadcasterID)
	if tally == nil {
		return
	}
	tally.flags++
	tally.enforced += enforcedDelta
	if tally.rules == nil {
		tally.rules = new([bktCount]int64)
	}
	tally.rules[b]++
}

func (s *botStats) channelTallyLocked(broadcasterID uint64) *chanTally {
	tally := s.channels[broadcasterID]
	if tally == nil {
		if len(s.channels) >= channelStatsMaxKeys {
			return nil
		}
		tally = &chanTally{}
		s.channels[broadcasterID] = tally
	}
	return tally
}

func (s *botStats) flush() {
	s.flushTotals()
	s.flushChannels()
}

func (s *botStats) flushTotals() {
	s.bump(counterEventsProcessed, s.events.Swap(0))
	s.bump(counterMessagesProcessed, s.messages.Swap(0))
	s.flushFlags()
}

func (s *botStats) flushFlags() {
	total := s.flagsTotal.Swap(0)
	if total == 0 {
		return
	}
	fields := make([]zap.Field, 0, bktCount+2)
	fields = append(fields,
		zap.Int64(flagFieldTotal, total),
		zap.Int64(flagFieldEnforced, s.flagsEnforced.Swap(0)))
	for i, name := range flagRuleNames {
		if d := s.flagsByRule[i].Swap(0); d != 0 {
			fields = append(fields, zap.Int64(flagFieldRulePfx+string(name), d))
		}
	}
	s.log.Debug("automod detection flags", fields...)
}

func (s *botStats) flushChannels() {
	s.mu.Lock()
	channels := s.channels
	if len(channels) > 0 {
		s.channels = map[uint64]*chanTally{}
	}
	s.mu.Unlock()

	var flagged []flagChannelEntry
	for id, tally := range channels {
		s.bumpChannel(id, counterEventsProcessed, tally.events)
		s.bumpChannel(id, counterMessagesProcessed, tally.messages)
		s.bumpChannel(id, counterCommandsAnswered, tally.answered)
		s.bumpChannel(id, counterModActions, tally.enforced)
		if tally.flags > 0 {
			flagged = append(flagged, flagChannelEntry{id: id, total: tally.flags, enforced: tally.enforced})
		}
	}
	if len(flagged) > 0 {
		s.log.Debug("automod detection flags by channel",
			zap.Array(flagFieldChannels, flagChannelArray(flagged)))
	}
}

type flagChannelEntry struct {
	id       uint64
	total    int64
	enforced int64
}

func (e flagChannelEntry) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddUint64(flagFieldChanID, e.id)
	enc.AddInt64(flagFieldTotal, e.total)
	enc.AddInt64(flagFieldEnforced, e.enforced)
	return nil
}

type flagChannelArray []flagChannelEntry

func (a flagChannelArray) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for i := range a {
		if err := enc.AppendObject(a[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *botStats) bump(name string, delta int64) {
	if delta == 0 {
		return
	}
	s.bumper.BumpBot(name, delta)
}

func (s *botStats) bumpChannel(broadcasterID uint64, name string, delta int64) {
	if delta == 0 {
		return
	}
	s.bumper.BumpChannel(broadcasterID, name, delta)
}

func (s *botStats) Close() {
	close(s.done)
	s.flush()
}
