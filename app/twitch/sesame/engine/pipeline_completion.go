// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"sync/atomic"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
)

func needsCompletion(d Deps, cfg Config) bool {
	_, bufferedLoyalty := d.Loyalty.(*ValkeyLoyaltyStore)
	for _, needed := range []bool{cfg.CountUses, d.Dedup != nil, d.Stats != nil, d.Analytics != nil, d.Seq != nil, d.ChatLines != nil, d.Reputation != nil, bufferedLoyalty} {
		if needed {
			return true
		}
	}
	return false
}

func (p *Pipeline) startCompletion(ctx context.Context, msg *bus.Message) (context.Context, *idempotency.Work) {
	ctx = msg.WithDeliveryContext(ctx)
	ctx, work := idempotency.NewWork(ctx)
	completion := msg.DeferCompletion()
	p.completions.add(completion)
	return ctx, work
}

type completionSet struct {
	once       sync.Once
	count      atomic.Int64
	progress   chan struct{}
	deliveries idempotency.Deliveries
}

func (s *completionSet) add(c *bus.Completion) {
	s.once.Do(func() {
		s.progress = make(chan struct{}, 1)
		s.deliveries.Retire = func() { s.remove(nil) }
	})
	s.count.Add(1)
}

func (s *completionSet) remove(c *bus.Completion) {
	s.count.Add(-1)
	progress := s.progress
	select {
	case progress <- struct{}{}:
	default:
	}
}

func (s *completionSet) remaining() int {
	return int(s.count.Load())
}

func (p *Pipeline) Finish(ctx context.Context) {
	flushed := make(chan struct{})
	go func() { p.flushReporters(ctx); close(flushed) }()
	select {
	case <-ctx.Done():
		return
	case <-flushed:
		p.completions.wait(ctx)
	}
}

func (p *Pipeline) flushReporters(ctx context.Context) {
	if p.stats != nil {
		p.stats.flush()
	}
	if p.uses != nil {
		p.uses.flushWith(ctx, counterPublicationAttempt{now: p.uses.clock(), final: true})
	}
	p.flushLoyalty(ctx)
	_ = p.analytics.Flush(ctx)
}

func (p *Pipeline) flushLoyalty(ctx context.Context) {
	reporter, _ := p.trialCounts.(*LoyaltyReporter)
	if reporter != nil {
		reporter.flushWith(ctx, counterPublicationAttempt{now: reporter.clock(), final: true})
	}
	store, ok := p.loyalty.(*ValkeyLoyaltyStore)
	if !ok {
		return
	}
	if store.reporter != reporter {
		store.reporter.flushWith(ctx, counterPublicationAttempt{now: store.reporter.clock(), final: true})
	}
}

func (s *completionSet) wait(ctx context.Context) {
	for s.remaining() > 0 {
		select {
		case <-s.progress:
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) countDecodedEffect(ctx context.Context, env *lane.Envelope, id uint64) {
	p.countDecoded(ctx, env, id)
}
