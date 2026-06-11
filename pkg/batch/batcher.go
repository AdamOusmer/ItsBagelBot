package batch

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Flush persists one batch, typically as a single bulk upsert.
type Flush[V any] func(ctx context.Context, items []V) error

// Batcher coalesces writes so the database sees one bulk statement per window
// instead of one round-trip per modification. Writes to the same key within a
// window collapse into the latest value, which is exactly what settings
// toggles produce when a user clicks around a dashboard.
//
// Durability trade-off: a value sits in memory for at most the flush interval
// before it is persisted, so this is only for state that can be re-submitted
// (configs, toggles, command edits). Money and tokens must not go through it.
type Batcher[K comparable, V any] struct {
	mu      sync.Mutex
	pending map[K]V

	flush    Flush[V]
	interval time.Duration
	maxSize  int

	kick chan struct{}
	stop chan struct{}
	done chan struct{}

	log *zap.Logger
}

// New starts a batcher that flushes whenever maxSize keys are pending or
// interval has elapsed since the previous flush, whichever comes first.
func New[K comparable, V any](interval time.Duration, maxSize int, flush Flush[V], log *zap.Logger) *Batcher[K, V] {

	b := &Batcher[K, V]{
		pending:  make(map[K]V, maxSize),
		flush:    flush,
		interval: interval,
		maxSize:  maxSize,
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
	b.mu.Unlock()

	if full {
		select {
		case b.kick <- struct{}{}:
		default: // a flush is already signalled
		}
	}
}

// Close flushes whatever is pending and stops the background loop.
func (b *Batcher[K, V]) Close(ctx context.Context) {

	close(b.stop)
	<-b.done

	b.flushPending(ctx)
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

	b.mu.Unlock()

	items := make([]V, 0, len(taken))
	for _, v := range taken {
		items = append(items, v)
	}

	if err := b.flush(ctx, items); err != nil {

		b.log.Error("batch flush failed, retrying next window",
			zap.Int("items", len(items)),
			zap.Error(err),
		)

		// Put the failed values back unless a newer write already replaced them.
		b.mu.Lock()
		for k, v := range taken {
			if _, exists := b.pending[k]; !exists {
				b.pending[k] = v
			}
		}
		b.mu.Unlock()
	}
}
