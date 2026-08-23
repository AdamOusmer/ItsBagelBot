// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"strings"
	"sync"
)

// rosterCapacityPerChannel ceilings one broadcaster's remembered chatter set.
// The roster exists so a {counter:target:...} token can key its bump on the
// mentioned viewer, and a mention is overwhelmingly someone currently in chat —
// a few thousand covers any channel's concurrent chatters with headroom. It is
// a cache of identities, not an archive: Twitch ids never change and every
// observed line refreshes the entry, so eviction only costs re-learning on the
// target's next line.
const rosterCapacityPerChannel = 4096

// chatterRoster remembers the viewers each replica has seen speak, per
// channel: login -> Viewer (the id keys viewer-scoped counter buckets; login
// and name ride along as the display identity). It is fed passively from every
// eligible chat line — the envelope already carries all three fields, so the
// hot path pays one sharded map store and nothing else — and read when a
// target-addressed counter token resolves who it counts against.
//
// Per-replica by design: sharing it through Valkey would put a network write on
// every chat line to serve a token that is best-effort anyway. A replica that
// has not seen the mentioned viewer speak falls back to sender-keyed counting
// (dispatch.go), which converges once the target says anything this replica
// observes.
type chatterRoster struct {
	mu    sync.RWMutex
	chans map[uint64]map[string]Viewer
}

func newChatterRoster() *chatterRoster {
	return &chatterRoster{chans: make(map[uint64]map[string]Viewer)}
}

// Observe records one chat line's speaker. Empty or unparseable identities are
// skipped: a bucket keyed by login must carry a real id or the bump it feeds
// would fall back to channel scope at flush time.
func (r *chatterRoster) Observe(broadcasterID uint64, login, id, name string) {
	if r == nil || broadcasterID == 0 || login == "" || id == "" {
		return
	}
	viewerID, err := strconv.ParseUint(id, 10, 64)
	if err != nil || viewerID == 0 {
		return
	}
	login = strings.ToLower(login)

	r.mu.Lock()
	defer r.mu.Unlock()
	chanViewers := r.chans[broadcasterID]
	if chanViewers == nil {
		chanViewers = make(map[string]Viewer)
		r.chans[broadcasterID] = chanViewers
	}
	if _, seen := chanViewers[login]; !seen && len(chanViewers) >= rosterCapacityPerChannel {
		// Evict one arbitrary entry (Go map range order is randomized) rather
		// than tracking recency: identities are stable, so any victim is as
		// good as another and the bookkeeping would outlive the benefit.
		for victim := range chanViewers {
			delete(chanViewers, victim)
			break
		}
	}
	// An empty display name (a folded-cohort sender carries none) must not
	// clobber the one a direct line already taught us.
	if prev, seen := chanViewers[login]; seen && prev.Name != "" && name == "" {
		name = prev.Name
	}
	chanViewers[login] = Viewer{ID: viewerID, Login: login, Name: name}
}

// Resolve looks up the mentioned viewer's identity. found=false leaves the
// caller on its fallback path; nothing here can fail loudly.
func (r *chatterRoster) Resolve(broadcasterID uint64, login string) (Viewer, bool) {
	if r == nil || broadcasterID == 0 || login == "" {
		return Viewer{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.chans[broadcasterID][strings.ToLower(login)]
	return v, ok
}
