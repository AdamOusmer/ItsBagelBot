package engine

import (
	"sync"
	"time"
)

// raidCooldown dedups a channel-level raid action (Shield Mode) so one raid does
// not re-fire it on every folded burst. It is a per-channel last-fired clock kept
// in process: sesame runs a few pods and Shield Mode activation is idempotent, so
// per-pod dedup is enough to collapse a raid's many folds into roughly one call.
type raidCooldown struct {
	mu   sync.Mutex
	last map[uint64]time.Time
	ttl  time.Duration
}

// pruneAbove bounds the map: once it holds more than this many channels, a trip
// sweeps out entries older than the cooldown so a long-lived process cannot leak
// one entry per channel it ever saw.
const raidCooldownPruneAbove = 1024

func newRaidCooldown(ttl time.Duration) *raidCooldown {
	return &raidCooldown{last: make(map[uint64]time.Time), ttl: ttl}
}

// trip reports whether a raid action may fire for this channel at now, recording
// the fire time when it returns true. Within ttl of the last fire it returns
// false. Safe for concurrent use.
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
