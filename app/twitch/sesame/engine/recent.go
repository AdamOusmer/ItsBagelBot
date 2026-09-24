// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/moderation"
)

const (
	recentTTL          = 10 * time.Minute
	recentRingCap      = 128
	recentShards       = 16
	recentMaxTextRunes = 200
	recentPruneEvery   = 1024
	recentChanCap      = 4096
)

type channelID uint64

type stamp int64

type recentEntry struct {
	at   stamp
	uid  uint64
	text string
	role module.Role
}

type chanRecent struct {
	buf  []recentEntry
	head int
	len  int
	last stamp
}

func (c *chanRecent) push(e recentEntry, cutoff stamp) {
	for c.len > 0 && c.buf[c.oldestIdx()].at < cutoff {
		c.len--
	}
	if c.buf == nil {
		c.buf = make([]recentEntry, 0, recentRingCap)
	}
	if c.len < recentRingCap {
		c.buf = append(c.buf, e)
		c.len++
	} else {
		c.buf[c.head] = e
	}
	c.head = (c.head + 1) % recentRingCap
	if e.at > c.last {
		c.last = e.at
	}
}

func (c *chanRecent) oldestIdx() int { return (c.head - c.len + recentRingCap) % recentRingCap }

type recentShard struct {
	mu      sync.Mutex
	chans   map[uint64]*chanRecent
	records int
}

type recentStore interface {
	Record(chanID channelID, env *lane.Envelope, now time.Time)
	Sweep(ctx context.Context, chanID channelID, phrase string, now time.Time) []RecentHit
}

type RecentLog struct {
	shards [recentShards]recentShard
}

func NewRecentLog() *RecentLog {
	l := &RecentLog{}
	for i := range l.shards {
		l.shards[i].chans = make(map[uint64]*chanRecent)
	}
	return l
}

// Text aliases the lane payload: safe only while pkg/bus never reuses a delivered Data buffer.
func chatEntriesFromEnvelope(env *lane.Envelope, now time.Time) []recentEntry {
	if env.Text == "" || isCommandShape(env.Text) {
		return nil
	}
	text := truncateRunes(env.Text, recentMaxTextRunes)
	at := stamp(now.UnixNano())
	var out []recentEntry
	add := func(id string, role module.Role) {
		if uid, ok := parseTwitchID(id); ok {
			out = append(out, recentEntry{at: at, uid: uid, text: text, role: role})
		}
	}
	if len(env.Senders) == 0 {
		add(env.ChatterUserID, module.ParseRole(*env))
	} else {
		for i := range env.Senders {
			s := &env.Senders[i]
			add(s.ChatterUserID, senderRole(env, s))
		}
	}
	return out
}

func (l *RecentLog) Record(chanID channelID, env *lane.Envelope, now time.Time) {
	entries := chatEntriesFromEnvelope(env, now)
	if entries == nil {
		return
	}
	at := entries[0].at
	cutoff := at - stamp(recentTTL)

	sh := &l.shards[uint64(chanID)%recentShards]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	c := sh.chans[uint64(chanID)]
	if c == nil {
		if len(sh.chans) >= recentChanCap {
			pruneChannels(sh, cutoff)
		}
		c = &chanRecent{}
		sh.chans[uint64(chanID)] = c
	}
	for _, e := range entries {
		c.push(e, cutoff)
	}

	sh.records++
	if sh.records%recentPruneEvery == 0 {
		pruneChannels(sh, cutoff)
	}
}

type RecentHit struct {
	UserID channelID
	Role   module.Role
}

func (l *RecentLog) Sweep(_ context.Context, chanID channelID, phrase string, now time.Time) []RecentHit {
	q := moderation.Normalize(GetBuf(), phrase)
	defer PutBuf(q)
	if utf8.RuneCount(q) == 0 {
		return nil
	}

	sh := &l.shards[uint64(chanID)%recentShards]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	c := sh.chans[uint64(chanID)]
	if c == nil {
		return nil
	}
	cutoff := stamp(now.Add(-recentTTL).UnixNano())
	hits := make([]RecentHit, 0, 16)
	seen := newUIDSet(16)
	t := GetBuf()
	for i := 0; i < c.len; i++ {
		e := &c.buf[(c.head-1-i+recentRingCap)%recentRingCap]
		if e.at < cutoff {
			break
		}
		t = moderation.Normalize(t, e.text)
		if !containsPhrase(t, q) || !seen.add(channelID(e.uid)) {
			continue
		}
		hits = append(hits, RecentHit{UserID: channelID(e.uid), Role: e.role})
	}
	PutBuf(t)
	return hits
}

func containsPhrase(text, phrase []byte) bool {
	if len(phrase) == 0 {
		return false
	}
	for off := 0; ; {
		i := bytes.Index(text[off:], phrase)
		if i < 0 {
			return false
		}
		s := off + i
		e := s + len(phrase)
		if atWordBoundary(text, s, e) {
			return true
		}
		off = s + 1
	}
}

func atWordBoundary(text []byte, s, e int) bool {
	if s > 0 && isWordByte(text[s-1]) {
		return false
	}
	return e >= len(text) || !isWordByte(text[e])
}

func isWordByte(b byte) bool {
	switch {
	case b >= utf8.RuneSelf:
		return true
	case b >= 'a' && b <= 'z', b >= '0' && b <= '9', b == '_':
		return true
	default:
		return false
	}
}

type uidSet map[channelID]struct{}

func newUIDSet(hint int) uidSet { return make(uidSet, hint) }

func (s uidSet) add(uid channelID) bool {
	if _, dup := s[uid]; dup {
		return false
	}
	s[uid] = struct{}{}
	return true
}

func isCommandShape(text string) bool {
	i := 0
	for i < len(text) && text[i] == ' ' {
		i++
	}
	return i < len(text) && text[i] == '!'
}

func senderRole(env *lane.Envelope, s *lane.Sender) module.Role {
	probe := lane.Envelope{ChatterUserID: s.ChatterUserID, BroadcasterUserID: env.BroadcasterUserID, Badges: s.Badges}
	return module.ParseRole(probe)
}

func truncateRunes(s string, max int) string {
	n := 0
	for i := range s {
		if n == max {
			return s[:i]
		}
		n++
	}
	return s
}

func parseTwitchID(s string) (uint64, bool) {
	uid, err := strconv.ParseUint(s, 10, 64)
	return uid, err == nil && uid != 0
}

func pruneChannels(sh *recentShard, cutoff stamp) {
	for id, c := range sh.chans {
		if c.last < cutoff {
			delete(sh.chans, id)
		}
	}
	for len(sh.chans) > recentChanCap {
		delete(sh.chans, stalestChannel(sh.chans))
	}
}

func stalestChannel(chans map[uint64]*chanRecent) uint64 {
	var staleID uint64
	var staleAt stamp
	first := true
	for id, c := range chans {
		if first || c.last < staleAt {
			staleID, staleAt, first = id, c.last, false
		}
	}
	return staleID
}
