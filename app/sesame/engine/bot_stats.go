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
	// botStatsFlushInterval is how often the two totals are handed to the
	// loyalty reporter, which batches again before it publishes. Short because
	// the flush costs two Bump calls regardless of how much traffic it folds.
	botStatsFlushInterval = 2 * time.Second

	// The counter names, read back by the dashboard through counter.get under
	// the reserved broadcaster-0 namespace (fleet totals) and through
	// counter.board across broadcasters (the per-channel split). The loyalty
	// service's bump flush creates the rows on first use, so neither needs a
	// counter.create; it also treats both names as system-owned, so a channel
	// cannot rewrite its own row.
	counterMessagesProcessed = data.CounterMessagesProcessed
	counterEventsProcessed   = data.CounterEventsProcessed

	// channelStatsFlushTicks is how many flush intervals the per-channel split
	// waits before it is published: 15 ticks, so every 30s. It rides a slower
	// clock than the fleet totals on purpose. The fleet pair is two counter
	// bumps whatever the traffic, but the split is two per *active channel*, and
	// each broadcaster's bumps leave the reporter as their own NATS message —
	// at the 2s cadence a few hundred live channels would turn a two-message
	// flush into a few hundred. The board it feeds is a lifetime ranking behind
	// a 15s page cache, so 30s of batching costs it nothing and cuts the
	// publish volume 15x. The counts themselves are unaffected: a longer window
	// folds more deltas into the same row.
	channelStatsFlushTicks = 15

	// channelStatsMaxKeys bounds the per-channel tally held between two
	// flushes. The fleet serves far fewer live channels than this in any
	// window, so the cap is a memory backstop against a pathological fan-out
	// (or a bug that mints broadcaster ids), not a working limit: a channel
	// turned away by a full map is simply counted in the next window.
	channelStatsMaxKeys = 4096

	// flagRuleSlotCap bounds the per-rule flag buckets at ~24 slots. Only
	// bktCount are used today; the rest is headroom so adding a rule never
	// grows the structure dynamically — the bucket set is closed over the
	// constants below, and anything unrecognized folds into "other".
	flagRuleSlotCap = 24
)

// The verdict rule strings the automod emits, mirrored as constants so the
// per-rule flag buckets stay in sync with what moderate.go logs. Sources:
// automod/rules.go floor categories ("ip_logger", "scam"), gate.go's
// heuristicVerdict/blockTermVerdict, lexVerdict's "lex:<cat>:<term>" prefixes,
// and moderate.go's council/reputation suffixes ("council:campaign",
// "+campaign", "+repeat"). A verdict carrying suffixes is classified by its
// base rule, so "scam+campaign+repeat" lands on scam: enumerating every
// suffix combination would triple the bucket set for little audit value.
const (
	ruleIPLogger       = "ip_logger"
	ruleScam           = "scam"
	ruleHeuristic      = "heuristic"
	ruleBlockTerm      = "block_term"
	ruleLexHate        = "lex:hate:"
	ruleLexHarassment  = "lex:harassment:"
	ruleLexSexual      = "lex:sexual:"
	ruleLexProfanity   = "lex:profanity:"
	ruleCouncil        = "council:campaign"
	ruleSuffixRepeat   = "+repeat"
	ruleSuffixCampaign = "+campaign"
	// ruleShieldMode mirrors outgress.TypeShieldMode: the mass-raid channel
	// escalation counted as its own detection event.
	ruleShieldMode = "shield_mode"
	ruleOther      = "other"
)

// flagRuleBucket indexes flagsByRule; the order defines the log-field names.
type flagRuleBucket int

const (
	bktIPLogger flagRuleBucket = iota
	bktScam
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

var flagRuleNames = [bktCount]string{
	bktIPLogger:      ruleIPLogger,
	bktScam:          ruleScam,
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

// flagBucket maps a full verdict rule string onto its bucket: strip the known
// escalation suffixes, then match the base exactly (floor/heuristic/block
// term/council) or by lexicon category prefix.
func flagBucket(rule string) flagRuleBucket {
	return baseRuleBucket(stripRuleSuffixes(rule))
}

// stripRuleSuffixes folds the known escalation suffixes off a verdict rule so
// classification sees its base. The suffixes stack in any order and number
// ("scam+campaign+repeat"), so the fold loops until neither matches; a verdict
// carrying suffixes is classified by its base rule, since enumerating every
// suffix combination would triple the bucket set for little audit value.
func stripRuleSuffixes(rule string) string {
	base := rule
	for {
		if s, ok := strings.CutSuffix(base, ruleSuffixRepeat); ok {
			base = s
			continue
		}
		if s, ok := strings.CutSuffix(base, ruleSuffixCampaign); ok {
			base = s
			continue
		}
		break
	}
	return base
}

// baseRuleBucket resolves a suffix-free base rule onto its bucket: matched
// exactly (floor categories, the heuristics, the block term verdict, the
// council escalation and shield mode) or by lexicon category prefix. Unknown
// rules fold into other.
func baseRuleBucket(base string) flagRuleBucket {
	switch {
	case base == ruleIPLogger:
		return bktIPLogger
	case base == ruleScam:
		return bktScam
	case base == ruleHeuristic:
		return bktHeuristic
	case base == ruleBlockTerm:
		return bktBlockTerm
	case base == ruleCouncil:
		return bktCouncil
	case base == ruleShieldMode:
		return bktShieldMode
	case strings.HasPrefix(base, ruleLexHate):
		return bktLexHate
	case strings.HasPrefix(base, ruleLexHarassment):
		return bktLexHarassment
	case strings.HasPrefix(base, ruleLexSexual):
		return bktLexSexual
	case strings.HasPrefix(base, ruleLexProfanity):
		return bktLexProfanity
	default:
		return bktOther
	}
}

// Log field names for the detection-flag flushes. Flags surface only as log
// fields — no loyalty counter rows — because new bot-namespace counter names
// would not join the SystemCounter set (internal/domain/event/data), i.e. they
// would be a loyalty-schema change without its protection.
const (
	flagFieldTotal    = "flags_total"
	flagFieldEnforced = "flags_enforced"
	flagFieldRulePfx  = "flag_rule_"
	flagFieldChannels = "channels"
	flagFieldChanID   = "broadcaster_id"
)

// botStats keeps sesame's bot-wide lifetime totals: every envelope the consumer
// decoded, and the chat subset of it. The hot path only touches the atomics — no lock, no map, no allocation — and a flusher goroutine swaps them
// onto the loyalty reporter, which owns the batching from there.
//
// The deltas are loss-tolerant by design: the reporter drops a window whose
// publish failed, so a bad minute costs counts, never correctness elsewhere.
type botStats struct {
	events   atomic.Int64
	messages atomic.Int64

	// Automod detection observability: every non-none verdict bumps the total,
	// an enforced one also the enforced count, and each lands on its rule's
	// bucket. Sized to flagRuleSlotCap so the structure itself carries the
	// documented bound; only the first bktCount buckets are named and swept.
	flagsTotal    atomic.Int64
	flagsEnforced atomic.Int64
	flagsByRule   [flagRuleSlotCap]atomic.Int64

	// The same totals split per broadcaster, which is what the public
	// stats board ranks. A map behind a mutex rather than more atomics: the
	// key set is discovered at runtime, and the lock is held for two adds on
	// a path that already costs a JSON decode.
	mu       sync.Mutex
	channels map[uint64]*chanTally

	log    *zap.Logger
	bumper CounterBumper
	done   chan struct{}
}

// chanTally is one channel's slice of the current flush window.
type chanTally struct {
	events   int64
	messages int64
	flags    int64
	enforced int64
	// rules is allocated lazily on a channel's first flagged line: most
	// channels are never moderated, so the common case pays nothing beyond
	// the two ints above.
	rules *[bktCount]int64
}

func newBotStats(bumper CounterBumper, log ...*zap.Logger) *botStats {
	// The logger is optional so tests can construct the sink bare; production
	// wiring (NewPipeline) always passes d.Log - a Nop here would silently
	// discard both detection-flag windows.
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

// count records one decoded envelope, fleet-wide and against the channel it
// came from. A nil receiver is the "no stats sink wired" case (tests, and any
// build without a reporter), so the hot path stays a single call either way.
// broadcasterID 0 is an envelope whose channel could not be read: it still
// counts fleet-wide, because the fleet totals are "everything that decoded".
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

// flag records one automod verdict: fleet total, enforced subset when the
// action was actually emitted, and the rule's bucket. broadcasterID 0 (an
// unreadable channel) still counts fleet-wide, like count.
func (s *botStats) flag(broadcasterID uint64, rule string, enforced bool) {
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

// channelTallyLocked returns the channel's row, or nil when the map hit its
// backstop cap; callers must hold s.mu.
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

// flush hands over everything pending, both clocks at once: the shutdown path
// and the tests, where waiting out the slow channel tick would be pointless.
func (s *botStats) flush() {
	s.flushTotals()
	s.flushChannels()
}

// flushTotals swaps both fleet totals onto the reporter under the reserved bot
// namespace (broadcaster 0, bot scope). A zero delta is skipped: the reporter
// ignores it anyway, and an idle window should touch nothing.
func (s *botStats) flushTotals() {
	s.bump(counterEventsProcessed, s.events.Swap(0))
	s.bump(counterMessagesProcessed, s.messages.Swap(0))
	s.flushFlags()
}

// flushFlags publishes the detection-flag window as log fields (the goal is
// auditable precision per rule, not new dashboard counters). An empty window
// logs nothing. Buckets are only swept when the total is nonzero — every flag
// increments both, so a zero total implies all-zero buckets.
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
			fields = append(fields, zap.Int64(flagFieldRulePfx+name, d))
		}
	}
	s.log.Debug("automod detection flags", fields...)
}

// flushChannels hands each channel's window to the reporter as two channel-scope
// counter bumps. The map is swapped out under the lock so the hot path never
// waits on the publish, and the reporter's own batching folds the per-channel
// rows into the same per-broadcaster events it already sends.
//
// Channels that saw automod verdicts additionally surface their flag split as
// one log line; per-rule precision stays on the fleet flush (which runs 15x
// more often) to keep this line bounded at three fields per entry.
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
		if tally.flags > 0 {
			flagged = append(flagged, flagChannelEntry{id: id, total: tally.flags, enforced: tally.enforced})
		}
	}
	if len(flagged) > 0 {
		s.log.Debug("automod detection flags by channel",
			zap.Array(flagFieldChannels, flagChannelArray(flagged)))
	}
}

// flagChannelEntry is one channel's slice of the flag window.
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

// Close stops the ticker and flushes the remainder.
func (s *botStats) Close() {
	close(s.done)
	s.flush()
}
