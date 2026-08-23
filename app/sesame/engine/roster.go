// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"strings"
	"sync"

	"ItsBagelBot/internal/domain/event/lane"
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

// ObserveEnvelope feeds every speaker of one chat envelope into the roster:
// the line's own chatter plus each sender of a folded duplicate cohort (a spam
// burst is exactly when mentions fly). Non-chat envelopes contribute nothing;
// identity-less lines are dropped by Observe.
func (r *chatterRoster) ObserveEnvelope(broadcasterID uint64, env *lane.Envelope) {
	if env.Type != chatType {
		return
	}
	r.Observe(broadcasterID, env.ChatterUserLogin, env.ChatterUserID, env.ChatterUserName)
	for i := range env.Senders {
		// A cohort sender carries no display name on the wire; Observe keeps
		// the one a direct line already taught us.
		r.Observe(broadcasterID, env.Senders[i].ChatterUserLogin, env.Senders[i].ChatterUserID, "")
	}
}

// Observe records one chat line's speaker under their lower-cased login.
func (r *chatterRoster) Observe(broadcasterID uint64, login, id, name string) {
	if r == nil {
		return
	}
	if broadcasterID == 0 {
		return
	}
	if login == "" {
		return
	}
	viewerID := parseChatterID(id)
	if viewerID == 0 {
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
	prev, seen := chanViewers[login]
	if !seen {
		if len(chanViewers) >= rosterCapacityPerChannel {
			evictOne(chanViewers)
		}
	}
	chanViewers[login] = Viewer{ID: viewerID, Login: login, Name: preferredName(name, prev.Name)}
}

// Resolve looks up the mentioned viewer's identity by login. found=false
// leaves the caller on its fallback path; nothing here can fail loudly.
func (r *chatterRoster) Resolve(broadcasterID uint64, login string) (Viewer, bool) {
	if r == nil {
		return Viewer{}, false
	}
	if broadcasterID == 0 {
		return Viewer{}, false
	}
	if login == "" {
		return Viewer{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.chans[broadcasterID][strings.ToLower(login)]
	return v, ok
}

// parseChatterID parses one wire chatter id, rejecting the empty and
// non-numeric shapes: a roster bucket must carry an id the counter buckets can
// key on, or the bump it feeds would fall back to channel scope at flush time.
func parseChatterID(id string) uint64 {
	viewerID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0
	}
	return viewerID
}

// preferredName keeps a learned display name when the newer observation
// carries none.
func preferredName(newName, storedName string) string {
	if newName != "" {
		return newName
	}
	return storedName
}

// evictOne drops one arbitrary entry to make room for an insert. Go map range
// order is randomized, so "arbitrary" is uniform-ish — good enough because
// identities are stable and eviction only costs re-learning on the victim's
// next line; recency tracking would outlive its benefit. The caller holds mu.
func evictOne(chanViewers map[string]Viewer) {
	for victim := range chanViewers {
		delete(chanViewers, victim)
		break
	}
}
