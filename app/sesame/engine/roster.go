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

// chatterIdentity is one speaker's identity as the chat envelope carries it:
// three loose strings on the wire (the stable login, its numeric Twitch id,
// the mutable display name), kept together so the roster's surface stays
// shape-based rather than string-based.
type chatterIdentity struct {
	id    string
	login string
	name  string
}

// rosterKey returns the roster's lookup key for this speaker — the lower-cased
// login — plus the numeric id that keys viewer-scoped counter buckets. Both
// come back zero-shaped when the identity is unusable (no login, or an
// unparseable/zero id): a bucket keyed by login must carry a real id or the
// bump it feeds would fall back to channel scope at flush time.
func (who chatterIdentity) rosterKey() (string, uint64) {
	if who.login == "" {
		return "", 0
	}
	viewerID, err := strconv.ParseUint(who.id, 10, 64)
	if err != nil {
		return "", 0
	}
	if viewerID == 0 {
		return "", 0
	}
	return strings.ToLower(who.login), viewerID
}

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
	if r == nil {
		return
	}
	if env.Type != chatType {
		return
	}
	r.Observe(broadcasterID, chatterIdentity{
		id:    env.ChatterUserID,
		login: env.ChatterUserLogin,
		name:  env.ChatterUserName,
	})
	for i := range env.Senders {
		// A cohort sender carries no display name on the wire; Observe keeps
		// the one a direct line already taught us.
		r.Observe(broadcasterID, chatterIdentity{
			id:    env.Senders[i].ChatterUserID,
			login: env.Senders[i].ChatterUserLogin,
		})
	}
}

// Observe records one speaker in their channel's set, keyed by the identity's
// lower-cased login.
func (r *chatterRoster) Observe(broadcasterID uint64, who chatterIdentity) {
	if broadcasterID == 0 {
		return
	}
	login, viewerID := who.rosterKey()
	if viewerID == 0 {
		return // covers the empty-login shape too: rosterKey zeroes both
	}

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
	entry := Viewer{ID: viewerID, Login: login, Name: who.name}
	if who.name == "" {
		// An unnamed observation (a folded-cohort sender carries no display
		// name) must not clobber the one a direct line already taught us.
		entry.Name = prev.Name
	}
	chanViewers[login] = entry
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
