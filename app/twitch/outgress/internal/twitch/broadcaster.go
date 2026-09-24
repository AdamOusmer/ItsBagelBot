// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"sync"
	"time"
)

const (
	maxBroadcasterSources = 2048
	sourceIdleTTL         = time.Hour
)

type sourceEntry struct {
	source   *Source
	lastUsed time.Time
}

type BroadcasterTokens struct {
	mu    sync.Mutex
	cache map[string]*sourceEntry
	build func(broadcasterID string) *Source
}

func NewBroadcasterTokens(build func(broadcasterID string) *Source) *BroadcasterTokens {
	return &BroadcasterTokens{cache: make(map[string]*sourceEntry), build: build}
}

func (b *BroadcasterTokens) Get(broadcasterID string) *Source {
	if b == nil || broadcasterID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if e, ok := b.cache[broadcasterID]; ok {
		e.lastUsed = now
		return e.source
	}
	if len(b.cache) >= maxBroadcasterSources {
		b.evictLocked(now)
	}
	s := b.build(broadcasterID)
	b.cache[broadcasterID] = &sourceEntry{source: s, lastUsed: now}
	return s
}

func (b *BroadcasterTokens) evictLocked(now time.Time) {
	oldestID := b.evictIdleLocked(now)
	if len(b.cache) >= maxBroadcasterSources && oldestID != "" {
		delete(b.cache, oldestID)
	}
}

func (b *BroadcasterTokens) evictIdleLocked(now time.Time) string {
	var oldestID string
	var oldestUse time.Time
	for id, e := range b.cache {
		if now.Sub(e.lastUsed) >= sourceIdleTTL {
			delete(b.cache, id)
			continue
		}
		if oldestID == "" || e.lastUsed.Before(oldestUse) {
			oldestID, oldestUse = id, e.lastUsed
		}
	}
	return oldestID
}

func (b *BroadcasterTokens) evictIdle(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evictIdleLocked(now)
}

const broadcasterSweepInterval = 2 * time.Minute

func (b *BroadcasterTokens) StartRefreshSweep(ctx context.Context) {
	go b.runRefreshSweep(ctx)
}

func (b *BroadcasterTokens) runRefreshSweep(ctx context.Context) {
	ticker := time.NewTicker(broadcasterSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.sweepTick(ctx)
		}
	}
}

func (b *BroadcasterTokens) sweepTick(ctx context.Context) {
	b.evictIdle(time.Now())
	b.sweepOnce(ctx)
}

const broadcasterSweepBudget = broadcasterSweepInterval

func (b *BroadcasterTokens) sweepOnce(ctx context.Context) {
	budgetCtx, cancel := context.WithTimeout(ctx, broadcasterSweepBudget)
	defer cancel()
	for _, s := range b.snapshotSources() {
		if budgetCtx.Err() != nil {
			return
		}
		s.refreshIfDue(budgetCtx)
	}
}

func (b *BroadcasterTokens) snapshotSources() []*Source {
	b.mu.Lock()
	defer b.mu.Unlock()
	sources := make([]*Source, 0, len(b.cache))
	for _, e := range b.cache {
		sources = append(sources, e.source)
	}
	return sources
}
