// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"sync"
	"time"

	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	blockedLogEvery   = time.Minute
	blockedLogMaxKeys = 512
)

type blockedKey struct{ broadcasterID, kind string }

type blockedTally struct {
	count      int64
	generation uint64
	endpoint   string
	method     string
	sample     []byte
}

// BlockedLog turns every blocked trial output into one summary line per channel and
// type per minute, so a spamming chat cannot grow log ingest with its message rate.
type BlockedLog struct {
	log     *zap.Logger
	mu      sync.Mutex
	pending map[blockedKey]*blockedTally
	dropped int64
	stop    chan struct{}
	done    chan struct{}
}

func NewBlockedLog(log *zap.Logger) *BlockedLog {
	b := &BlockedLog{log: log, pending: map[blockedKey]*blockedTally{}, stop: make(chan struct{}), done: make(chan struct{})}
	go b.run()
	return b
}

func (b *BlockedLog) Record(m *outgress.Message) {
	if b == nil {
		return
	}
	key := blockedKey{m.BroadcasterID, m.Type}
	b.mu.Lock()
	defer b.mu.Unlock()
	tally, ok := b.pending[key]
	if !ok {
		if len(b.pending) >= blockedLogMaxKeys {
			b.dropped++
			return
		}
		tally = &blockedTally{generation: m.TrialGeneration, endpoint: m.Endpoint, method: m.Method, sample: append([]byte(nil), m.Payload...)}
		b.pending[key] = tally
	}
	tally.count++
}

func (b *BlockedLog) Close() {
	if b == nil {
		return
	}
	close(b.stop)
	<-b.done
}

func (b *BlockedLog) run() {
	defer close(b.done)
	ticker := time.NewTicker(blockedLogEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.Flush()
		case <-b.stop:
			b.Flush()
			return
		}
	}
}

func (b *BlockedLog) Flush() {
	pending, dropped := b.swap()
	for key, tally := range pending {
		b.log.Info("trial output blocked",
			zap.String("broadcaster_id", key.broadcasterID),
			zap.Uint64("trial_generation", tally.generation),
			zap.String("type", key.kind),
			zap.Int64("count", tally.count),
			zap.Duration("window", blockedLogEvery),
			zap.String("endpoint", tally.endpoint),
			zap.String("method", tally.method),
			zap.ByteString("sample_payload", tally.sample))
	}
	if dropped > 0 {
		b.log.Warn("trial output blocked beyond the summary key cap", zap.Int64("count", dropped))
	}
}

func (b *BlockedLog) swap() (map[blockedKey]*blockedTally, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	pending, dropped := b.pending, b.dropped
	b.pending, b.dropped = map[blockedKey]*blockedTally{}, 0
	return pending, dropped
}
