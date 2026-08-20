// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/data"
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
)

// botStats keeps sesame's bot-wide lifetime totals: every envelope the consumer
// decoded, and the chat subset of it. The hot path only touches the two
// atomics — no lock, no map, no allocation — and a flusher goroutine swaps them
// onto the loyalty reporter, which owns the batching from there.
//
// The deltas are loss-tolerant by design: the reporter drops a window whose
// publish failed, so a bad minute costs counts, never correctness elsewhere.
type botStats struct {
	events   atomic.Int64
	messages atomic.Int64

	// The same two totals split per broadcaster, which is what the public
	// stats board ranks. A map behind a mutex rather than more atomics: the
	// key set is discovered at runtime, and the lock is held for two adds on
	// a path that already costs a JSON decode.
	mu       sync.Mutex
	channels map[uint64]*chanTally

	bumper CounterBumper
	done   chan struct{}
}

// chanTally is one channel's slice of the current flush window.
type chanTally struct {
	events   int64
	messages int64
}

func newBotStats(bumper CounterBumper) *botStats {
	s := &botStats{bumper: bumper, done: make(chan struct{}), channels: map[uint64]*chanTally{}}
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

func (s *botStats) countChannel(broadcasterID uint64, isChat bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tally := s.channels[broadcasterID]
	if tally == nil {
		if len(s.channels) >= channelStatsMaxKeys {
			return
		}
		tally = &chanTally{}
		s.channels[broadcasterID] = tally
	}
	tally.events++
	if isChat {
		tally.messages++
	}
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
}

// flushChannels hands each channel's window to the reporter as two channel-scope
// counter bumps. The map is swapped out under the lock so the hot path never
// waits on the publish, and the reporter's own batching folds the per-channel
// rows into the same per-broadcaster events it already sends.
func (s *botStats) flushChannels() {
	s.mu.Lock()
	channels := s.channels
	if len(channels) > 0 {
		s.channels = map[uint64]*chanTally{}
	}
	s.mu.Unlock()

	for id, tally := range channels {
		s.bumpChannel(id, counterEventsProcessed, tally.events)
		s.bumpChannel(id, counterMessagesProcessed, tally.messages)
	}
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
