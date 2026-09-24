// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"

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
	pub  bus.Publisher
	log  *zap.Logger
	done chan struct{}

	mu   sync.Mutex
	pend map[useKey]uint64
}

func newUseReporter(pub bus.Publisher, log *zap.Logger) *useReporter {
	r := &useReporter{
		pub:  pub,
		log:  log,
		done: make(chan struct{}),
		pend: map[useKey]uint64{},
	}
	go func() {
		ticker := time.NewTicker(useFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
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
	if _, tracked := r.pend[key]; !tracked && len(r.pend) >= useMaxKeys {
		r.mu.Unlock()
		go r.flush(context.Background())
		return
	}
	r.pend[key]++
	r.mu.Unlock()
}

func (r *useReporter) flush(ctx context.Context) {
	r.mu.Lock()
	if len(r.pend) == 0 {
		r.mu.Unlock()
		return
	}
	pend := r.pend
	r.pend = map[useKey]uint64{}
	r.mu.Unlock()

	for key, n := range pend {
		if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandUsed, data.CommandUsedDTO{
			UserID: key.userID,
			Name:   key.name,
			Count:  n,
		}); err != nil {
			r.log.Debug("failed to publish command uses",
				module.BIDField(key.userID),
				zap.String("command", key.name),
				zap.Uint64("count", n),
				zap.Error(err),
			)
		}
	}
}

func (r *useReporter) Close() {
	close(r.done)
	r.flush(context.Background())
}
