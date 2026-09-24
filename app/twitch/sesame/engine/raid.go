// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"
	"time"
)

type raidCooldown struct {
	mu   sync.Mutex
	last map[uint64]time.Time
	ttl  time.Duration
}

const raidCooldownPruneAbove = 1024

func newRaidCooldown(ttl time.Duration) *raidCooldown {
	return &raidCooldown{last: make(map[uint64]time.Time), ttl: ttl}
}

func (r *raidCooldown) trip(broadcasterID uint64, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.last[broadcasterID]; ok && now.Sub(t) < r.ttl {
		return false
	}
	r.last[broadcasterID] = now
	if len(r.last) > raidCooldownPruneAbove {
		for k, t := range r.last {
			if now.Sub(t) >= r.ttl {
				delete(r.last, k)
			}
		}
	}
	return true
}
