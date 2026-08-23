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

// Flush persists one batch, typically as a single bulk upsert.
type Flush[V any] func(ctx context.Context, items []V) error

// defaultFlushTimeout bounds one flush callback. Every flush runs on the
// batcher's single goroutine, and pending memory is bounded only by distinct-key
// cardinality, so a database that accepts the connection but never answers would
// otherwise pin that goroutine forever while Add keeps accumulating windows it
// will never drain. 5s covers every flush statement in the fleet with headroom;
// a slower flush means the database is already failing, and failing fast loses
// at most one window while keeping later ones flowing.
const defaultFlushTimeout = 5 * time.Second

// Stats is a point-in-time snapshot of the batcher's write-behind behaviour,
// for gauges and alerting: Pending depth signals staleness risk, Failures in a
// row signal a database refusing writes while callers see success.
type Stats struct {
	Pending      int64
	Flushes      uint64
	Failures     uint64
	ItemsFlushed uint64
	LastDuration time.Duration
}

// Batcher coalesces writes so the database sees one bulk statement per window
// instead of one round-trip per modification. Writes to the same key within a
// window collapse into the latest value, which is exactly what settings
// toggles produce when a user clicks around a dashboard.
//
// Durability trade-off: a value sits in memory for at most the flush interval
// before it is persisted, so this is only for state that can be re-submitted
// (configs, toggles, command edits). Money and tokens must not go through it.
//
// Memory ceiling: pending is bounded by the number of DISTINCT keys written
// within a flush window (maxSize forces a drain at that count), never by write
// rate -- a burst of writes to the same key replaces in place.
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

	// guards stop: Close must tolerate being called twice (owner + cleanup).
	closeOnce sync.Once

	log *zap.Logger

	// Telemetry, all monotonic except the gauge. Read through Stats.
	pendingGauge atomic.Int64
	flushes      atomic.Uint64
	failures     atomic.Uint64
	itemsFlushed atomic.Uint64
	lastNanos    atomic.Int64
}

// New starts a batcher that flushes whenever maxSize keys are pending or
// interval has elapsed since the previous flush, whichever comes first.
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

// Add queues value under key, replacing any pending value for the same key.
func (b *Batcher[K, V]) Add(key K, value V) {

	b.mu.Lock()
	b.pending[key] = value
	full := len(b.pending) >= b.maxSize
	b.pendingGauge.Store(int64(len(b.pending)))
	b.mu.Unlock()

	if full {
		select {
		case b.kick <- struct{}{}:
		default: // a flush is already signalled
		}
	}
}

// Requeue restores a value whose flush failed transiently, unless a newer
// write for the same key arrived while the flush ran — the newer value wins,
// exactly like the batcher's own whole-batch retry. Flush callbacks use this
// to retry individual items instead of failing the entire batch.
func (b *Batcher[K, V]) Requeue(key K, value V) {

	b.mu.Lock()
	if _, exists := b.pending[key]; !exists {
		b.pending[key] = value
	}
	// A failed flush requeues through here after flushPending zeroed the
	// gauge; without this, Stats reports an empty backlog while retries sit
	// in pending and staleness alerts stay silent.
	b.pendingGauge.Store(int64(len(b.pending)))
	b.mu.Unlock()
}

// closeRetryBackoff spaces out final-drain retries in Close so a database
// that stopped answering is not hammered in a tight loop for the whole
// shutdown budget.
const closeRetryBackoff = 100 * time.Millisecond

// Close flushes whatever is pending and stops the background loop. Idempotent:
// an owner and a deferred cleanup may both call it without sequencing care.
//
// The final drain retries until the caller's context is spent: once stop has
// fired, no later window can consume requeued items, so a transient failure
// here would otherwise drop accepted writes at process exit. If the budget
// runs out first, what remains is logged as lost — never returned silently.
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

	// The deadline is applied here, around whatever context the caller handed
	// in, so Close's final drain is bounded by the same ceiling as the run
	// loop: caller cancellation still propagates through the parent.
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

		// Put the failed values back unless a newer write already replaced them.
		b.mu.Lock()
		for k, v := range taken {
			if _, exists := b.pending[k]; !exists {
				b.pending[k] = v
			}
		}
		b.pendingGauge.Store(int64(len(b.pending)))
		b.mu.Unlock()
		return
	}

	// A window that takes longer than the interval means the flusher can no
	// longer keep up with the write rate: windows queue behind it and the
	// staleness guarantee (interval, not interval x windows) silently breaks.
	if elapsed > b.interval {
		b.log.Warn("batch flush exceeded its window",
			zap.Int("items", len(items)),
			zap.Duration("elapsed", elapsed),
			zap.Duration("interval", b.interval),
		)
	}
}

// Stats returns a snapshot of the batcher's behaviour for dashboards and
// alerting. Counters are monotonic for the life of the batcher.
func (b *Batcher[K, V]) Stats() Stats {
	return Stats{
		Pending:      b.pendingGauge.Load(),
		Flushes:      b.flushes.Load(),
		Failures:     b.failures.Load(),
		ItemsFlushed: b.itemsFlushed.Load(),
		LastDuration: time.Duration(b.lastNanos.Load()),
	}
}
