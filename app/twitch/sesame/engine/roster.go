// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"strings"
	"sync"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
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

type chanRoster struct {
	list    []Viewer
	byLogin map[string]int
	byID    map[uint64]int
}

func newChanRoster() *chanRoster {
	return &chanRoster{byLogin: make(map[string]int), byID: make(map[uint64]int)}
}

// put keeps one slot per login and per id; Slot's O(1) exclusion depends on it.
func (c *chanRoster) put(entry Viewer) {
	if slot, seen := c.byID[entry.ID]; seen && c.list[slot].Login != entry.Login {
		c.remove(slot)
	}
	if slot, seen := c.byLogin[entry.Login]; seen && c.list[slot].ID != entry.ID {
		c.remove(slot)
	}
	if slot, seen := c.byLogin[entry.Login]; seen {
		c.list[slot] = entry
		return
	}
	if len(c.list) >= rosterCapacityPerChannel {
		c.evictOne()
	}
	c.byLogin[entry.Login] = len(c.list)
	c.byID[entry.ID] = len(c.list)
	c.list = append(c.list, entry)
}

func (c *chanRoster) remove(slot int) {
	victim := c.list[slot]
	last := len(c.list) - 1
	moved := c.list[last]
	c.list[slot] = moved
	c.byLogin[moved.Login] = slot
	c.byID[moved.ID] = slot
	c.list = c.list[:last]
	delete(c.byLogin, victim.Login)
	delete(c.byID, victim.ID)
}

func (c *chanRoster) evictOne() {
	for _, slot := range c.byLogin {
		c.remove(slot)
		return
	}
}

type chatterRoster struct {
	mu    sync.RWMutex
	chans map[uint64]*chanRoster
}

func newChatterRoster() *chatterRoster {
	return &chatterRoster{chans: make(map[uint64]*chanRoster)}
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
		chanViewers = newChanRoster()
		r.chans[broadcasterID] = chanViewers
	}
	entry := Viewer{ID: viewerID, Login: login, Name: who.name}
	if slot, seen := chanViewers.byLogin[login]; seen && who.name == "" {
		entry.Name = chanViewers.list[slot].Name
	}
	chanViewers.put(entry)
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
	chanViewers := r.chans[broadcasterID]
	if chanViewers == nil {
		return Viewer{}, false
	}
	slot, ok := chanViewers.byLogin[strings.ToLower(login)]
	if !ok {
		return Viewer{}, false
	}
	return chanViewers.list[slot], true
}

func (r *chatterRoster) View(broadcasterID uint64, read func(scope.RosterView)) {
	if r == nil || broadcasterID == 0 {
		read(chanRosterView{})
		return
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	read(chanRosterView{r.chans[broadcasterID]})
}

type chanRosterView struct{ c *chanRoster }

func (v chanRosterView) Len() int {
	if v.c == nil {
		return 0
	}
	return len(v.c.list)
}

func (v chanRosterView) At(i int) scope.Chatter {
	who := v.c.list[i]
	return scope.Chatter{ID: who.ID, Name: chatterName(who)}
}

func (v chanRosterView) Slot(id uint64) (int, bool) {
	if v.c == nil {
		return 0, false
	}
	slot, ok := v.c.byID[id]
	return slot, ok
}
