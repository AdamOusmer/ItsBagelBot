// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Flush[V any] func(ctx context.Context, items []V) error

const defaultFlushTimeout = 5 * time.Second

type Stats struct {
	Pending      int64
	Flushes      uint64
	Failures     uint64
	ItemsFlushed uint64
	LastDuration time.Duration
}

// Pending writes live only in memory until flushed; never use it for money or tokens.
type Batcher[K comparable, V any] struct {
	mu      sync.Mutex
	pending map[K]V

	flush    Flush[V]
	interval time.Duration
	maxSize  int
	deadline time.Duration

	kick chan struct{}
	stop chan struct{}
	done chan struct{}

	closeOnce sync.Once

	log *zap.Logger

	pendingGauge atomic.Int64
	flushes      atomic.Uint64
	failures     atomic.Uint64
	itemsFlushed atomic.Uint64
	lastNanos    atomic.Int64
}

func New[K comparable, V any](interval time.Duration, maxSize int, flush Flush[V], log *zap.Logger) *Batcher[K, V] {

	b := &Batcher[K, V]{
		pending:  make(map[K]V, maxSize),
		flush:    flush,
		interval: interval,
		maxSize:  maxSize,
		deadline: defaultFlushTimeout,
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		log:      log,
	}

	go b.run()

	return b
}

func (b *Batcher[K, V]) Add(key K, value V) {

	b.mu.Lock()
	b.pending[key] = value
	full := len(b.pending) >= b.maxSize
	b.pendingGauge.Store(int64(len(b.pending)))
	b.mu.Unlock()

	if full {
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
}

func (b *Batcher[K, V]) restoreUnlessReplacedLocked(key K, value V) {
	if _, exists := b.pending[key]; !exists {
		b.pending[key] = value
	}
}

func (b *Batcher[K, V]) Requeue(key K, value V) {
	b.mu.Lock()
	b.restoreUnlessReplacedLocked(key, value)
	b.pendingGauge.Store(int64(len(b.pending)))
	b.mu.Unlock()
}

const closeRetryBackoff = 100 * time.Millisecond

func (b *Batcher[K, V]) Close(ctx context.Context) {

	b.closeOnce.Do(func() { close(b.stop) })
	<-b.done

	for b.pendingCount() > 0 {
		b.flushPending(ctx)

		if b.pendingCount() == 0 {
			return
		}

		select {
		case <-ctx.Done():
			b.log.Error("batcher closed before pending writes landed; writes lost",
				zap.Int("items", b.pendingCount()))
			return
		case <-time.After(closeRetryBackoff):
		}
	}
}

func (b *Batcher[K, V]) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *Batcher[K, V]) run() {

	defer close(b.done)

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
		case <-b.kick:
		}

		b.flushPending(context.Background())
	}
}

func (b *Batcher[K, V]) flushPending(ctx context.Context) {

	b.mu.Lock()

	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}

	taken := b.pending
	b.pending = make(map[K]V, b.maxSize)
	b.pendingGauge.Store(0)

	b.mu.Unlock()

	items := make([]V, 0, len(taken))
	for _, v := range taken {
		items = append(items, v)
	}

	fctx, cancel := context.WithTimeout(ctx, b.deadline)
	defer cancel()

	start := time.Now()
	err := b.flush(fctx, items)
	elapsed := time.Since(start)

	b.flushes.Add(1)
	b.itemsFlushed.Add(uint64(len(items)))
	b.lastNanos.Store(int64(elapsed))

	if err != nil {

		b.failures.Add(1)

		b.log.Error("batch flush failed, retrying next window",
			zap.Int("items", len(items)),
			zap.Duration("elapsed", elapsed),
			zap.Error(err),
		)

		b.mu.Lock()
		for k, v := range taken {
			b.restoreUnlessReplacedLocked(k, v)
		}
		b.pendingGauge.Store(int64(len(b.pending)))
		b.mu.Unlock()
		return
	}

	if elapsed > b.interval {
		b.log.Warn("batch flush exceeded its window",
			zap.Int("items", len(items)),
			zap.Duration("elapsed", elapsed),
			zap.Duration("interval", b.interval),
		)
	}
}

func (b *Batcher[K, V]) Stats() Stats {
	return Stats{
		Pending:      b.pendingGauge.Load(),
		Flushes:      b.flushes.Load(),
		Failures:     b.failures.Load(),
		ItemsFlushed: b.itemsFlushed.Load(),
		LastDuration: time.Duration(b.lastNanos.Load()),
	}
}
