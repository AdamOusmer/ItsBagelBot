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

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/moderation"
)

// The recent-chat log is the memory behind the !nuke sweep: a bounded,
// in-process per-channel ring of the last few seconds of plain chat, written
// on the pipeline hot path and read only when a moderator nukes. Everything
// about its geometry follows the learned layers' memory discipline
// (vocab.go / baseline.go): shard, cap, TTL, prune lazily, allocate nothing
// steady-state.
const (
	// recentTTL is how long a recorded line stays eligible for a nuke. A raid
	// wave is acted on within seconds-to-minutes; ten minutes matches the
	// campaign HLL's sliding TTL so every "recent" window in the automod stack
	// tells the same story.
	recentTTL = 10 * time.Minute
	// recentRingCap caps the lines kept per channel. During a raid a channel
	// blows through 128 lines in seconds — which is exactly the point: the
	// ring holds the raid's tail (the last ~seconds before the mod types
	// !nuke), not the stream's whole history. 128 entries × ≤200 runes keeps
	// a channel at roughly a dozen KB.
	recentRingCap = 128
	// recentShards spreads the per-channel mutexes; chat partitions by
	// broadcaster across consumer goroutines, so contention per shard is low.
	recentShards = 16
	// recentMaxTextRunes truncates stored text. Matching runs on the head of
	// the message; spam waves repeat their payload early, and capping bounds
	// both the copy cost per record and the normalize cost per swept line.
	recentMaxTextRunes = 200
	// recentPruneEvery records-per-shard between whole-channel prunes. Idle
	// channels then die within one TTL without any timer goroutine.
	recentPruneEvery = 1024
	// recentChanCap bounds channels per shard, same geometry as
	// vocabChanCap: it only bites if 4096 channels are genuinely chatting
	// within one TTL window on one replica, and the prune pass sheds them.
	recentChanCap = 4096
)

// stamp is a unix-nano instant in the recent window's clock domain. A named
// integer keeps the ring's expiry arithmetic (record cutoffs, idle prunes,
// TTL comparisons) from blending with unrelated int64s — the type system now
// carries the distinction the comments used to.
type stamp int64

// recentEntry is one retained chat line: who said it, when, with what trust
// level, and the truncated text. The user id is stored parsed because Helix
// takes numeric ids anyway and a uint64 never dangles — envelope strings are
// zero-copy views into a recycled lane payload, so anything retained past the
// handler must be copied or parsed.
type recentEntry struct {
	at   stamp // unix nanos
	uid  uint64
	text string
	role module.Role
}

// chanRecent is one channel's FIFO ring. buf grows lazily up to
// recentRingCap and then wraps: head always marks the next write slot, so
// during growth head == len and after full the write overwrites the oldest.
type chanRecent struct {
	buf  []recentEntry
	head int
	len  int
	last stamp // newest entry's timestamp; the channel-prune key
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

// recentShard owns one slice of the channel keyspace.
type recentShard struct {
	mu      sync.Mutex
	chans   map[uint64]*chanRecent
	records int
}

// recentStore is the sweep memory behind the Nuke service. Implementations:
// RecentLog (in-process, single-replica / tests) and ValkeyRecent (the
// centralized store production runs — see recent_valkey.go for why a replica
// pool cannot share an in-memory window).
type recentStore interface {
	Record(chanID uint64, env *lane.Envelope, now time.Time)
	Sweep(ctx context.Context, chanID uint64, phrase string, now time.Time) []RecentHit
}

// RecentLog is the IN-MEMORY recentStore: correct only when a single sesame
// replica consumes a channel's chat, which makes it the test double. Production
// runs a replica pool sharing one durable JetStream consumer (pkg/bus), so any
// pod sees an arbitrary fraction of a channel's lines — the centralized
// ValkeyRecent is what ships (recent_valkey.go).
type RecentLog struct {
	shards [recentShards]recentShard
}

// NewRecentLog builds an empty log.
func NewRecentLog() *RecentLog {
	l := &RecentLog{}
	for i := range l.shards {
		l.shards[i].chans = make(map[uint64]*chanRecent)
	}
	return l
}

// chatEntriesFromEnvelope parses one chat envelope into retainable entries —
// the solo sender, or every sender of a folded cohort (sharing one text view).
// Command-shaped lines are skipped: a mod must never be nuked by the very
// command line they typed to invoke it, and squashed cohorts are plain chat
// anyway. nil when nothing is retainable.
//
// The retained text aliases the lane payload's bytes, which is deliberate: the
// transport never reuses a delivered payload buffer (pkg/bus pullEnvelopePool
// recycles only the nats.Msg struct and leaves Data uncleared precisely so
// delivered Messages may keep aliasing it), so Go's collector owns the
// lifetime and no copy is needed on the hot path.
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

// Record retains one chat envelope: the solo sender, or every sender of a
// folded cohort (which share one text copy).
func (l *RecentLog) Record(chanID uint64, env *lane.Envelope, now time.Time) {
	entries := chatEntriesFromEnvelope(env, now)
	if entries == nil {
		return
	}
	at := entries[0].at
	cutoff := at - stamp(recentTTL)

	sh := &l.shards[chanID%recentShards]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	c := sh.chans[chanID]
	if c == nil {
		if len(sh.chans) >= recentChanCap {
			pruneChannels(sh, cutoff)
		}
		c = &chanRecent{}
		sh.chans[chanID] = c
	}
	for _, e := range entries {
		c.push(e, cutoff)
	}

	sh.records++
	if sh.records%recentPruneEvery == 0 {
		pruneChannels(sh, cutoff)
	}
}

// RecentHit is one distinct sender whose recent line matched a nuke phrase.
type RecentHit struct {
	UserID uint64
	Role   module.Role
}

// Sweep returns the distinct senders within the TTL whose retained line
// contains phrase, newest first. Matching is normalized on both sides
// (moderation.Normalize: case, leet, confusables, zero-width) at token
// boundaries, so "FR33 N1TRO" matches "free nitro" while "ass" misses "bass".
// The accepted residual mirrors the floor's documented one: fused tokens
// ("freenitro") miss — splitting fused words needs edit-distance machinery
// whose false-positive rate no nuke should carry.
func (l *RecentLog) Sweep(_ context.Context, chanID uint64, phrase string, now time.Time) []RecentHit {
	q := moderation.Normalize(GetBuf(), phrase)
	defer PutBuf(q)
	if utf8.RuneCount(q) == 0 {
		return nil
	}

	sh := &l.shards[chanID%recentShards]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	c := sh.chans[chanID]
	if c == nil {
		return nil
	}
	cutoff := stamp(now.Add(-recentTTL).UnixNano())
	hits := make([]RecentHit, 0, 16)
	t := GetBuf()
	// The ring bounds the walk (≤ recentRingCap entries), so no separate
	// result cap exists: a channel can never surface more senders than it
	// retained lines.
	for i := 0; i < c.len; i++ {
		e := &c.buf[(c.head-1-i+recentRingCap)%recentRingCap]
		if e.at < cutoff {
			break // walking newest→oldest: everything further back is expired
		}
		t = moderation.Normalize(t, e.text)
		if !containsPhrase(t, q) || seenUID(hits, e.uid) {
			continue
		}
		hits = append(hits, RecentHit{UserID: e.uid, Role: e.role})
	}
	PutBuf(t)
	return hits
}

// containsPhrase reports whether phrase occurs in text at word boundaries.
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

// atWordBoundary reports whether text[s:e] stands as its own token: neither
// edge may continue a word character.
func atWordBoundary(text []byte, s, e int) bool {
	if s > 0 && isWordByte(text[s-1]) {
		return false
	}
	return e >= len(text) || !isWordByte(text[e])
}

// isWordByte treats any non-ASCII byte as part of a word (conservative: a
// CJK/Arabic neighbor must not mint a boundary) alongside ASCII letters,
// digits and underscore — the alphabet normalization leaves behind.
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

func seenUID(hits []RecentHit, uid uint64) bool {
	for i := range hits {
		if hits[i].UserID == uid {
			return true
		}
	}
	return false
}

// isCommandShape mirrors parseCommand's trigger rule without parsing: any
// '!'-leading line is a command candidate and stays out of the buffer.
func isCommandShape(text string) bool {
	i := 0
	for i < len(text) && text[i] == ' ' {
		i++
	}
	return i < len(text) && text[i] == '!'
}

// senderRole resolves a folded cohort sender's trust tier. ParseRole reads an
// Envelope, so the sender's fields ride a value-shaped probe — no heap.
func senderRole(env *lane.Envelope, s *lane.Sender) module.Role {
	probe := lane.Envelope{ChatterUserID: s.ChatterUserID, BroadcasterUserID: env.BroadcasterUserID, Badges: s.Badges}
	return module.ParseRole(probe)
}

// truncateRunes returns the first max runes of s (zero-copy; the caller copies).
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

// pruneChannels drops channels idle past the TTL and, if still over the cap,
// the stalest remainder. Called under the shard lock, amortized once per
// recentPruneEvery records.
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

// stalestChannel finds the channel whose newest line is oldest. Callers hold
// the shard lock; the map is never empty when this runs (the caller only asks
// while over the cap).
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
