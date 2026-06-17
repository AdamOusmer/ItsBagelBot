package twitch

import "sync"

// BroadcasterTokens lazily builds and caches one user-token Source per
// broadcaster id, so "send as the broadcaster" jobs reuse a single refreshing
// token per channel instead of re-minting on every message. The built Source
// loads the channel's refresh token from the users service on each renewal, so
// a channel that authorizes later starts working without a restart (and one
// that never authorized simply errors at send time).
type BroadcasterTokens struct {
	mu    sync.Mutex
	cache map[string]*Source
	build func(broadcasterID string) *Source
}

func NewBroadcasterTokens(build func(broadcasterID string) *Source) *BroadcasterTokens {
	return &BroadcasterTokens{cache: make(map[string]*Source), build: build}
}

// Get returns the cached Source for a broadcaster, building it on first use.
func (b *BroadcasterTokens) Get(broadcasterID string) *Source {
	if b == nil || broadcasterID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.cache[broadcasterID]; ok {
		return s
	}
	s := b.build(broadcasterID)
	b.cache[broadcasterID] = s
	return s
}
