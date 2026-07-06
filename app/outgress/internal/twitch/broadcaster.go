package twitch

import (
	"sync"
	"time"
)

// The token cache is bounded so a long tail of one-off broadcaster jobs cannot
// grow the map for the life of the process. Evicting a Source is safe: the
// refresh token lives in the users service, so a rebuilt Source resumes from
// the stored grant on its next renewal.
const (
	maxBroadcasterSources = 2048
	sourceIdleTTL         = time.Hour
)

type sourceEntry struct {
	source   *Source
	lastUsed time.Time
}

// BroadcasterTokens lazily builds and caches one user-token Source per
// broadcaster id, so "send as the broadcaster" jobs reuse a single refreshing
// token per channel instead of re-minting on every message. The built Source
// loads the channel's refresh token from the users service on each renewal, so
// a channel that authorizes later starts working without a restart (and one
// that never authorized simply errors at send time).
type BroadcasterTokens struct {
	mu    sync.Mutex
	cache map[string]*sourceEntry
	build func(broadcasterID string) *Source
}

func NewBroadcasterTokens(build func(broadcasterID string) *Source) *BroadcasterTokens {
	return &BroadcasterTokens{cache: make(map[string]*sourceEntry), build: build}
}

// Get returns the cached Source for a broadcaster, building it on first use.
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

// evictLocked drops every source idle past sourceIdleTTL, falling back to the
// least recently used one so an insert never grows the map past the cap. A
// caller already holding an evicted *Source keeps using it safely; only the
// cache slot is released.
func (b *BroadcasterTokens) evictLocked(now time.Time) {
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
	if len(b.cache) >= maxBroadcasterSources && oldestID != "" {
		delete(b.cache, oldestID)
	}
}
