// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"github.com/google/uuid"

	"go.uber.org/zap"
)

const (
	useFlushInterval = 5 * time.Second

	useMaxKeys = 1024
)

type useKey struct {
	userID uint64
	name   string
}

type useReporter struct {
	pub      bus.Publisher
	log      *zap.Logger
	done     chan struct{}
	finished chan struct{}
	wake     chan struct{}
	wakeup   counterReporterWake
	flushMu  sync.Mutex
	pending  counterPublications
	now      func() time.Time
	nextLog  time.Time

	mu   sync.Mutex
	pend map[useKey]int64
}

func newUseReporter(pub bus.Publisher, log *zap.Logger) *useReporter {
	r := &useReporter{
		pub:      pub,
		now:      time.Now,
		log:      log,
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		wake:     make(chan struct{}, 1),
		pend:     map[useKey]int64{},
	}
	go func() {
		defer close(r.finished)
		ticker := time.NewTicker(useFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.flush(context.Background())
			case <-r.wake:
				r.wakeup.flush(r.flush)
			case <-r.done:
				return
			}
		}
	}()
	return r
}

func (r *useReporter) Record(userID uint64, name string) {
	if userID == 0 || name == "" {
		return
	}
	key := useKey{userID: userID, name: name}

	r.mu.Lock()
	if r.pend[key] >= data.MaxCounter {
		r.mu.Unlock()
		r.log.Error("command use window exceeds exact integer range")
		return
	}
	r.pend[key]++
	full := len(r.pend) >= useMaxKeys
	r.mu.Unlock()
	if full {
		r.wakeup.nudge(r.wake)
	}
}

func (r *useReporter) flush(ctx context.Context) {
	r.flushWith(ctx, counterPublicationAttempt{now: r.clock()})
}

func (r *useReporter) flushWith(ctx context.Context, attempt counterPublicationAttempt) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	defer r.reportBacklog()
	if !attempt.final {
		r.pending = r.pending.retry(ctx, r.pub, r.log, attempt)
	}
	if !attempt.drains(r.pending) {
		return
	}
	r.mu.Lock()
	pend := r.pend
	r.pend = map[useKey]int64{}
	r.mu.Unlock()
	for key, n := range pend {
		batchID := uuid.NewString()
		r.pending = append(r.pending, counterPublication{id: batchID, subject: data.SubjectCommandUsed, entries: 1, firstAttempt: r.clock(), payload: data.CommandUsedDTO{
			BatchID: batchID, UserID: key.userID, Name: key.name, Count: n,
		}})
	}
	r.pending = r.pending.retry(ctx, r.pub, r.log, attempt)
}

func (r *useReporter) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

func (r *useReporter) backlog() counterReporterBacklog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return counterReporterBacklog{uses: len(r.pend), pending: len(r.pending)}
}

func (r *useReporter) reportBacklog() {
	backlog := r.backlog()
	r.wakeup.pending.Store(backlog.pending > 0)
	backlog.log(r.log, &r.nextLog, r.clock())
}

func (r *useReporter) Close() {
	close(r.done)
	<-r.finished
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.flushWith(ctx, counterPublicationAttempt{now: r.clock(), final: true})
	r.backlog().shutdown(r.log)
}
