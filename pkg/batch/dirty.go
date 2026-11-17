// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Dirty[K comparable] struct {
	mu     sync.Mutex
	keys   map[K]struct{}
	closed bool
	wake   chan struct{}
	done   chan struct{}
	flush  func([]K) []K
	log    *zap.Logger
}

func NewDirty[K comparable](flush func([]K) []K, log *zap.Logger) *Dirty[K] {
	d := &Dirty[K]{keys: make(map[K]struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}), flush: flush, log: log}
	go d.run()
	return d
}

func (d *Dirty[K]) Mark(keys []K) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		d.log.Error("dirty keys marked after close", zap.Int("keys", len(keys)))
		return
	}
	d.mark(keys)
}

func (d *Dirty[K]) mark(keys []K) {
	for _, key := range keys {
		d.keys[key] = struct{}{}
	}
	d.signal()
}

func (d *Dirty[K]) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dirty[K]) take() ([]K, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	keys := make([]K, 0, len(d.keys))
	for key := range d.keys {
		keys = append(keys, key)
	}
	clear(d.keys)
	if d.closed {
		d.signal()
	}
	return keys, d.closed && len(keys) == 0
}

func (d *Dirty[K]) run() {
	defer close(d.done)
	backoff := closeRetryBackoff
	for {
		<-d.wake
		keys, closed := d.take()
		if closed {
			return
		}
		failed := d.publish(keys)
		if len(failed) == 0 {
			backoff = closeRetryBackoff
			continue
		}
		time.Sleep(backoff)
		backoff = min(2*backoff, 5*time.Second)
		d.mu.Lock()
		d.mark(failed)
		d.mu.Unlock()
	}
}

func (d *Dirty[K]) publish(keys []K) (failed []K) {
	if len(keys) == 0 {
		return nil
	}
	defer func() {
		if p := recover(); p != nil {
			d.log.Error("dirty key publication panic", zap.Any("panic", p))
			failed = keys
		}
	}()
	return d.flush(keys)
}

func (d *Dirty[K]) Close(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	d.signal()
	d.mu.Unlock()
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
