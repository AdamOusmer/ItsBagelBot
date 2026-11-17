// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"go.uber.org/zap"
)

// Never mutate a pending payload or batch ID: an uncertain publish may already have committed.
type counterPublication struct {
	id           string
	subject      string
	payload      any
	entries      int
	firstAttempt time.Time
	lastAttempt  time.Time
	nextAttempt  time.Time
	retryDelay   time.Duration
	nextLog      time.Time
}

type counterPublications []counterPublication

const counterPublicationLogInterval = time.Minute

type counterPublicationAttempt struct {
	now   time.Time
	final bool
}

func (a counterPublicationAttempt) drains(pending counterPublications) bool {
	return a.final || len(pending) == 0
}

func (pending counterPublications) retry(ctx context.Context, pub bus.Publisher, log *zap.Logger, attempt counterPublicationAttempt) counterPublications {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	started := pending.begin(ctx, pub, attempt)
	remaining := pending[:0]
	for i, publication := range pending {
		if i >= len(started) {
			return append(remaining, pending[i:]...)
		}
		if publication.finish(ctx, started[i], log, attempt.now) {
			continue
		}
		remaining = append(remaining, publication)
	}
	clear(pending[len(remaining):])
	if len(remaining) == 0 {
		return nil
	}
	return remaining
}

type counterFuture struct {
	future bus.PublishFuture
	err    error
}

func (pending counterPublications) begin(ctx context.Context, pub bus.Publisher, attempt counterPublicationAttempt) []counterFuture {
	started := make([]counterFuture, 0, len(pending))
	for i := range pending {
		p := &pending[i]
		if !attempt.final && attempt.now.Before(p.nextAttempt) {
			break
		}
		p.lastAttempt = attempt.now
		if p.firstAttempt.IsZero() {
			p.firstAttempt = attempt.now
		}
		started = append(started, p.begin(ctx, pub))
	}
	return started
}

func (p *counterPublication) begin(ctx context.Context, pub bus.Publisher) counterFuture {
	body, err := codec.FastMarshal(p.payload)
	if err != nil {
		return counterFuture{err: err}
	}
	future, err := bus.BeginConfirmed(ctx, pub, bus.Publication{Subject: p.subject, ID: p.id, Payload: body})
	return counterFuture{future: future, err: err}
}

func (p *counterPublication) finish(ctx context.Context, started counterFuture, log *zap.Logger, now time.Time) bool {
	err := started.err
	if err == nil {
		err = started.future.Await(ctx)
	}
	if err == nil {
		return true
	}
	p.scheduleRetry(now)
	p.logRetry(log, now, err)
	return false
}

func (p *counterPublication) scheduleRetry(now time.Time) {
	p.retryDelay = min(max(5*time.Second, 2*p.retryDelay), 30*time.Second)
	p.nextAttempt = now.Add(p.retryDelay)
}

func (p *counterPublication) logRetry(log *zap.Logger, now time.Time, err error) {
	if now.Before(p.nextLog) {
		return
	}
	p.nextLog = now.Add(counterPublicationLogInterval)
	log.Error("loyalty batch retained for retry", zap.String("subject", p.subject), zap.String("batch_id", p.id), zap.Int("entries", p.entries), zap.Duration("age", now.Sub(p.firstAttempt)), zap.String("error_class", counterPublishErrorClass(err)), zap.Error(err))
}

func counterPublishErrorClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return fmt.Sprintf("%T", err)
	}
}

type counterReporterWake struct {
	pending atomic.Bool
}

func (w *counterReporterWake) nudge(wake chan struct{}) {
	if w.pending.Load() {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (w *counterReporterWake) flush(flush func(context.Context)) {
	if w.pending.Load() {
		return
	}
	flush(context.Background())
}

type counterReporterBacklog struct {
	bumps, earn, uses, pending int
}

func (b counterReporterBacklog) fields() []zap.Field {
	return []zap.Field{zap.Int("bumps", b.bumps), zap.Int("earn", b.earn), zap.Int("uses", b.uses), zap.Int("pending", b.pending)}
}

func (b counterReporterBacklog) log(log *zap.Logger, nextLog *time.Time, now time.Time) {
	if b.pending == 0 || now.Before(*nextLog) {
		return
	}
	*nextLog = now.Add(counterPublicationLogInterval)
	log.Warn("counter reporter backlog", b.fields()...)
}

func (b counterReporterBacklog) shutdown(log *zap.Logger) {
	if b.bumps+b.earn+b.uses+b.pending == 0 {
		return
	}
	log.Error("counter reporter work remains unconfirmed at shutdown", b.fields()...)
}
