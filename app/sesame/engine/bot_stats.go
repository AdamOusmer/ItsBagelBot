// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync/atomic"
	"time"
)

const (
	// botStatsFlushInterval is how often the two totals are handed to the
	// loyalty reporter, which batches again before it publishes. Short because
	// the flush costs two Bump calls regardless of how much traffic it folds.
	botStatsFlushInterval = 2 * time.Second

	// The bot-scope counter names, read back by the dashboard through
	// counter.get under the reserved broadcaster-0 namespace. The loyalty
	// service's bump flush creates the rows on first use, so neither needs a
	// counter.create.
	counterMessagesProcessed = "messages_processed"
	counterEventsProcessed   = "events_processed"
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

	bumper CounterBumper
	done   chan struct{}
}

func newBotStats(bumper CounterBumper) *botStats {
	s := &botStats{bumper: bumper, done: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(botStatsFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.flush()
			case <-s.done:
				return
			}
		}
	}()
	return s
}

// count records one decoded envelope. A nil receiver is the "no stats sink
// wired" case (tests, and any build without a reporter), so the hot path stays
// a single call either way.
func (s *botStats) count(isChat bool) {
	if s == nil {
		return
	}
	s.events.Add(1)
	if isChat {
		s.messages.Add(1)
	}
}

// flush swaps both totals onto the reporter under the reserved bot namespace
// (broadcaster 0, bot scope). A zero delta is skipped: the reporter ignores it
// anyway, and an idle window should touch nothing.
func (s *botStats) flush() {
	s.bump(counterEventsProcessed, s.events.Swap(0))
	s.bump(counterMessagesProcessed, s.messages.Swap(0))
}

func (s *botStats) bump(name string, delta int64) {
	if delta == 0 {
		return
	}
	s.bumper.BumpBot(name, delta)
}

// Close stops the ticker and flushes the remainder.
func (s *botStats) Close() {
	close(s.done)
	s.flush()
}
