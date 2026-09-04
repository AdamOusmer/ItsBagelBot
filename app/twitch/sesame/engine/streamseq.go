// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"
)

// The consumer pool hands stream events to whichever goroutine is free
// (pkg/bus/weighted.go: handlers must be safe for concurrent use), so two
// lifecycle events for the same broadcaster — stream.online then an immediate
// stream.offline — can run their fire-and-forget follow-ups interleaved, and a
// late ArmAll / tick Arm lands after the offline's DisarmAll (#561). The
// Sequencer is the intra-replica ordering fix: per-broadcaster FIFO queues in
// which each enqueued task runs to completion before the next starts.
//
// Cross-replica ordering is NOT provided here (two replicas may receive the
// pair); that half belongs to the versioned live-key writes. What this buys is
// exactly the effects that have no version: timer/loyalty arm-disarm and greet
// resets land in event order on the replica that saw both events.

const (
	// seqQueueDepth bounds one broadcaster's backlog. Lifecycle events are a
	// handful per stream; the depth only has to absorb a burst, and an overflow
	// degrades to inline execution rather than dropping state changes.
	seqQueueDepth = 64
)

// Sequencer runs enqueued tasks per broadcaster strictly in arrival order.
type Sequencer struct {
	mu   sync.Mutex
	seqs map[uint64]*seqQueue
}

// seqQueue is one broadcaster's FIFO plus the flag marking a live pump. mu
// guards busy and the map membership; the channel is only sent to under mu so
// a reaping pump cannot race an enqueue into a queue it just abandoned.
type seqQueue struct {
	ch   chan func()
	busy bool
}

// NewSequencer builds an empty sequencer.
func NewSequencer() *Sequencer {
	return &Sequencer{seqs: make(map[uint64]*seqQueue)}
}

// Do enqueues task behind everything previously enqueued for broadcasterID.
// It never blocks on task execution — the handler returns immediately — but it
// does block briefly on the internal mutex, which no task holds while running.
// When a broadcaster's backlog is full the task degrades to inline execution:
// ordering with the queued tasks is lost for that one task, but dropping a
// ClearLive or a DisarmAll would be strictly worse.
func (s *Sequencer) Do(broadcasterID uint64, task func()) {
	if broadcasterID == 0 || task == nil {
		return
	}
	s.mu.Lock()
	q, ok := s.seqs[broadcasterID]
	if !ok {
		q = &seqQueue{ch: make(chan func(), seqQueueDepth)}
		s.seqs[broadcasterID] = q
	}
	select {
	case q.ch <- task:
		if !q.busy {
			q.busy = true
			s.mu.Unlock()
			go s.pump(broadcasterID, q)
			return
		}
		s.mu.Unlock()
	default:
		// Backlog full: run inline rather than drop (see Do's doc).
		s.mu.Unlock()
		task()
	}
}

// pump drains one broadcaster's queue until it is momentarily empty, then
// leaves the map so the next event spawns a fresh pump. Every enqueue happens
// under s.mu, so the emptiness check and the delete cannot race a sender: if a
// task was enqueued first, len > 0 and we keep going; if the delete happened
// first, the sender built a fresh queue.
func (s *Sequencer) pump(broadcasterID uint64, q *seqQueue) {
	for {
		var task func()
		s.mu.Lock()
		select {
		case task = <-q.ch:
			s.mu.Unlock()
		default:
			delete(s.seqs, broadcasterID)
			q.busy = false
			s.mu.Unlock()
			return
		}
		task()
	}
}
