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
	remaining := pending[:0]
	for i, publication := range pending {
		if !attempt.final && attempt.now.Before(publication.nextAttempt) {
			return append(remaining, pending[i:]...)
		}
		if publication.publish(ctx, pub, log, attempt.now) {
			continue
		}
		remaining = append(remaining, publication)
		if !attempt.final {
			return append(remaining, pending[i+1:]...)
		}
	}
	clear(pending[len(remaining):])
	if len(remaining) == 0 {
		return nil
	}
	return remaining
}

func (p *counterPublication) publish(ctx context.Context, pub bus.Publisher, log *zap.Logger, now time.Time) bool {
	if p.firstAttempt.IsZero() {
		p.firstAttempt = now
	}
	attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
	body, err := codec.FastMarshal(p.payload)
	if err == nil {
		err = bus.PublishConfirmed(attempt, pub, bus.Publication{Subject: p.subject, ID: p.id, Payload: body})
	}
	cancel()
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
