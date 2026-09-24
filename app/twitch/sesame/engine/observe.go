// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type ObservedEvent struct {
	BroadcasterID uint64
	Type          string
	At            time.Time
	Locale        string

	Handled    bool
	Command    string
	Actor      string
	DurationMS int
}

type Observer interface {
	Observe(ev ObservedEvent)
}

const observerQueueDepth = 256

type observerLane struct {
	obs     Observer
	ch      chan ObservedEvent
	dropped atomic.Uint64
}

func (p *Pipeline) RegisterObserver(o Observer) {
	lane := &observerLane{obs: o, ch: make(chan ObservedEvent, observerQueueDepth)}
	p.observers = append(p.observers, lane)
	go lane.run(p.log)
}

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

func (p *Pipeline) notifyObservers(ev ObservedEvent) {
	if len(p.observers) == 0 {
		return
	}
	ev.Type = strings.Clone(ev.Type)
	ev.Locale = strings.Clone(ev.Locale)
	ev.Command = strings.Clone(ev.Command)
	ev.Actor = strings.Clone(ev.Actor)

	for _, l := range p.observers {
		select {
		case l.ch <- ev:
		default:
			l.dropped.Add(1)
		}
	}
}

func (p *Pipeline) closeObservers() {
	for _, l := range p.observers {
		close(l.ch)
	}
	p.observers = nil
}
