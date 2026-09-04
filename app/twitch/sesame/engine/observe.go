// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ObservedEvent is what Process hands every registered Observer once a message
// has run its stages. It carries the outcome, not just the arrival: the
// activity feed needs the command that answered and how long it took, and a
// pre-stage hook cannot know either.
//
// Every string here is a COPY. The envelope Process decodes is pooled and its
// strings are zero-copy views into the lane payload (see decodeEnvelope), which
// is owned only for the synchronous handler — PutEnvelope runs on defer the
// moment Process returns. Handing a view to an observer that outlives the call
// is a use-after-free, so notifyObservers clones before it sends.
type ObservedEvent struct {
	BroadcasterID uint64
	Type          string
	At            time.Time

	// Handled is true when the line dispatched a command that ran. Command is
	// the trigger without its slash/bang, empty when the line was not one.
	Handled    bool
	Command    string
	Actor      string
	DurationMS int
}

// Observer receives one ObservedEvent per processed message, in order.
//
// This is a registration point, and deliberately a wide one: the per-minute
// chat-volume lane and the activity-feed lane each register their own Observer
// and neither edits Process again. That only holds if this struct already
// carries what both need — a thinner event would push the feed lane back into
// pipeline.go, which is the coupling this hook exists to prevent.
type Observer interface {
	Observe(ev ObservedEvent)
}

// observerQueue is one observer's bounded mailbox. Depth is per observer, so a
// slow consumer cannot make the pipeline allocate without limit; it drops.
const observerQueueDepth = 256

type observerLane struct {
	obs     Observer
	ch      chan ObservedEvent
	dropped atomic.Uint64
}

// RegisterObserver adds an observer and starts its consumer. Call it during
// wiring, before the pipeline consumes; it is not safe to call concurrently
// with Process.
func (p *Pipeline) RegisterObserver(o Observer) {
	lane := &observerLane{obs: o, ch: make(chan ObservedEvent, observerQueueDepth)}
	p.observers = append(p.observers, lane)
	go lane.run(p.log)
}

// run drains one observer's mailbox on a single goroutine, so events reach the
// observer in the order Process saw them. A feed rendered from out-of-order
// rows is wrong, which rules out a goroutine per event.
func (l *observerLane) run(log *zap.Logger) {
	defer func() {
		if r := recover(); r != nil && log != nil {
			log.Error("pipeline: observer panicked", zap.Any("panic", r))
		}
	}()
	for ev := range l.ch {
		l.obs.Observe(ev)
	}
}

// notifyObservers hands the event to every observer's mailbox without blocking.
//
// Process runs on the busiest path in the fleet — 833 ns/op and 12 allocs/op on
// BenchmarkProcessNoOutputWithViews, held there by a pooled envelope, pooled
// module views and a zero-copy decode. A goroutine per event per observer would
// cost more than every allocation that benchmark fights for (~2 KB of stack
// each, unbounded under load) and would deliver out of order besides. A bounded
// channel per observer costs one send and drops under backpressure instead.
func (p *Pipeline) notifyObservers(ev ObservedEvent) {
	if len(p.observers) == 0 {
		return
	}
	// Clone once, not per observer: the payload views die with this call.
	ev.Type = strings.Clone(ev.Type)
	ev.Command = strings.Clone(ev.Command)
	ev.Actor = strings.Clone(ev.Actor)

	for _, l := range p.observers {
		select {
		case l.ch <- ev:
		default:
			// Telemetry is not worth stalling chat for. Dropping is the
			// designed behaviour under backpressure; the counter makes it
			// visible rather than silent.
			l.dropped.Add(1)
		}
	}
}

// closeObservers stops every consumer. Called from Pipeline.Close.
func (p *Pipeline) closeObservers() {
	for _, l := range p.observers {
		close(l.ch)
	}
	p.observers = nil
}
