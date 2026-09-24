// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"strings"
	"sync"

	"ItsBagelBot/internal/domain/event/lane"
)

const rosterCapacityPerChannel = 4096

type chatterIdentity struct {
	id    string
	login string
	name  string
}

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

type chatterRoster struct {
	mu    sync.RWMutex
	chans map[uint64]map[string]Viewer
}

func newChatterRoster() *chatterRoster {
	return &chatterRoster{chans: make(map[uint64]map[string]Viewer)}
}

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
		r.Observe(broadcasterID, chatterIdentity{
			id:    env.Senders[i].ChatterUserID,
			login: env.Senders[i].ChatterUserLogin,
		})
	}
}

func (r *chatterRoster) Observe(broadcasterID uint64, who chatterIdentity) {
	if broadcasterID == 0 {
		return
	}
	login, viewerID := who.rosterKey()
	if viewerID == 0 {
		return
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
		entry.Name = prev.Name
	}
	chanViewers[login] = entry
}

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

func (r *chatterRoster) Snapshot(broadcasterID uint64) []Viewer {
	if r == nil || broadcasterID == 0 {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	chanViewers := r.chans[broadcasterID]
	out := make([]Viewer, 0, len(chanViewers))
	for _, v := range chanViewers {
		out = append(out, v)
	}
	return out
}

func evictOne(chanViewers map[string]Viewer) {
	for victim := range chanViewers {
		delete(chanViewers, victim)
		break
	}
}
