package module

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

const (
	// useFlushInterval bounds the bus rate: a spammed zero-cooldown command
	// costs one summed event per window instead of one per execution.
	useFlushInterval = 5 * time.Second

	// useMaxKeys caps the pending map. At the cap the reporter flushes early;
	// if a flush is already running, further ticks for NEW keys are dropped
	// (counters are loss-tolerant; protecting the worker's memory wins).
	useMaxKeys = 1024
)

type useKey struct {
	userID uint64
	name   string
}

// useReporter aggregates command-use ticks per (broadcaster, command) and
// publishes summed data.commands.used events on a flush window. It is the
// worker-side rate limiter for the counter pipeline.
type useReporter struct {
	pub  message.Publisher
	log  *zap.Logger
	done chan struct{}

	mu   sync.Mutex
	pend map[useKey]uint64
}

func newUseReporter(pub message.Publisher, log *zap.Logger) *useReporter {
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

// Record counts one execution. Never blocks the chat hot path: it takes a
// short mutex, and at the map cap it either triggers an early flush or drops
// the tick for a not-yet-tracked key.
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

// flush drains the pending sums and publishes one event per command.
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
				zap.Uint64("broadcaster_id", key.userID),
				zap.String("command", key.name),
				zap.Uint64("count", n),
				zap.Error(err),
			)
		}
	}
}

// Close stops the ticker and flushes what is pending.
func (r *useReporter) Close() {
	close(r.done)
	r.flush(context.Background())
}
