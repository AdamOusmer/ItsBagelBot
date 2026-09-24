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
	flushMu  sync.Mutex
	pending  []counterPublication

	mu   sync.Mutex
	pend map[useKey]int64
}

func newUseReporter(pub bus.Publisher, log *zap.Logger) *useReporter {
	r := &useReporter{
		pub:      pub,
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
				r.flush(context.Background())
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
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

func (r *useReporter) flush(ctx context.Context) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	r.pending = retryCounterPublications(ctx, r.pub, r.log, r.pending)
	if len(r.pending) > 0 {
		return
	}
	r.mu.Lock()
	pend := r.pend
	r.pend = map[useKey]int64{}
	r.mu.Unlock()
	for key, n := range pend {
		batchID := uuid.NewString()
		r.pending = append(r.pending, counterPublication{id: batchID, subject: data.SubjectCommandUsed, payload: data.CommandUsedDTO{
			BatchID: batchID, UserID: key.userID, Name: key.name, Count: n,
		}})
	}
	r.pending = retryCounterPublications(ctx, r.pub, r.log, r.pending)
}

func (r *useReporter) Close() {
	close(r.done)
	<-r.finished
	r.flush(context.Background())
	if len(r.pending) > 0 {
		r.log.Error("command use batches remain unconfirmed at shutdown", zap.Int("batches", len(r.pending)))
	}
}
