// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package confcache memoizes the parse of a module's Configs blob. A
// broadcaster's config changes when they save the dashboard - roughly never,
// on the timescale of chat - yet the parse ran on EVERY chat message: measured
// on an M1 Pro (2026-09-03, BenchmarkProcessNoOutputWithAutomodConfig) a
// representative automod blob cost 2.2us, 1.5KB and 27 allocations per line,
// which is ~3.5x the entire rest of the no-output hot path.
package confcache

import "sync"

// KEY CHOICE: the cache is keyed on the CONTENT of the raw blob, not on
// (broadcasterID, module, revision).
//
// ModuleView.Revision is the obvious key and it is wrong here: its own doc
// comment says it is "Omitted (0) for legacy rows", so every legacy row in the
// fleet carries revision 0. A revision key would make two DIFFERENT configs
// that both sit at 0 collide, and the failure mode is not a stale parse - it
// is one channel's automod block-terms being enforced against another
// channel's chat. Rejected outright rather than mitigated: a fast path that is
// only correct while no legacy row exists is a bug waiting on a data
// migration, and internal/projection's evictScope drops the whole-set modules
// key without consulting payload.Keys, so nothing downstream guarantees a
// revision bump is even observed here.
//
// Content keying has none of that: the key IS the input to the pure function
// being memoized, so a hit is by construction a hit on the same bytes. Two
// broadcasters with byte-identical configs correctly share one parse, and an
// edit that leaves the revision at 0 still lands on a different key.
//
// It is spelled as a map[string] read through c.m[string(raw)]: the compiler
// elides the string copy on a []byte-keyed map lookup, so the steady-state hit
// path allocates nothing and the runtime's own (SIMD) string hash plus the
// memequal on collision do the content comparison. Rejected: hashing the blob
// into a uint64 key ourselves (a 64-bit key must still be verified against the
// bytes to rule out a collision that would leak one channel's config into
// another, so it buys nothing over the map the runtime already gives us);
// rejected: keying on the backing-array pointer of mv.Configs, which the
// projection cache does hold stable between edits - it works only for as long
// as nobody ever hands this a blob built some other way, and the cost of being
// wrong is again cross-channel config bleed.

// maxEntries caps the cache at distinct config BLOBS, not broadcasters: the
// dashboard writes the same defaults for most channels, so the entry count
// tracks how many channels actually customized the module rather than how many
// channels exist. 1024 blobs at the low-KB size a module form can produce is a
// couple of MB per cache, which is the ceiling worth paying to keep the parse
// off the chat path; the cap exists because the alternative to a bound is
// unbounded growth from a channel that rewrites its config in a loop.
const maxEntries = 1024

// entry is an intrusive LRU node as well as a cached parse: prev/next thread it
// onto the recency queue and key lets eviction delete the map slot without a
// reverse lookup. Same shape as app/twitch/sesame/automod/linkcheck's cache, which is
// the house pattern for a bounded map with O(1) eviction; the difference is
// that a parsed config has no TTL (it is valid exactly as long as its bytes
// are what the projection serves), so recency replaces linkcheck's per-TTL
// class queues and one list suffices.
type entry[T any] struct {
	val  T
	key  string
	prev *entry[T]
	next *entry[T]
}

// Cache memoizes parse results for config blobs of one module. The zero value
// is not usable; call New.
//
// A plain mutex guards it. Reads are a map lookup (~20ns) against a parse
// measured in microseconds, and the lock is never held across the parse
// itself, so the only contention is between the map operations of concurrent
// broadcasters. Rejected: sync.Map, whose read-mostly fast path would help
// only if the LRU bookkeeping went away, and dropping the LRU means dropping
// the bound.
//
// The cached value is IMMUTABLE and shared by every concurrent reader of the
// same blob. A consumer that mutates what Get returns corrupts every other
// channel using an identical config; callers that need to vary a field make
// their own copy (see engine.disabledConfig).
type Cache[T any] struct {
	mu   sync.Mutex
	m    map[string]*entry[T]
	head *entry[T] // least recently used: the next eviction
	tail *entry[T] // most recently used
}

// New returns an empty cache for parse results of type T.
func New[T any]() *Cache[T] {
	return &Cache[T]{m: make(map[string]*entry[T], 64)}
}

// Get returns the memoized parse of raw, calling parse only on a miss. parse
// MUST be a pure function of raw - that is what makes the content key sound.
//
// The parse runs OUTSIDE the lock, so two goroutines that miss on the same
// blob at the same moment may both parse it. That is deliberate: the duplicate
// work is bounded by one parse, both results are equal by purity, and the
// alternative (holding the mutex across the parse) parks every other
// broadcaster's chat line behind it.
func (c *Cache[T]) Get(raw []byte, parse func([]byte) T) T {
	// An absent config is the common case for a module a channel never
	// configured. Both parse functions answer it from a length check, so
	// caching it would trade a free call for a lock acquisition.
	if len(raw) == 0 {
		return parse(raw)
	}
	if v, ok := c.lookup(raw); ok {
		return v
	}
	v := parse(raw)
	c.store(raw, v)
	return v
}

// lookup returns the cached parse for raw, promoting it to most-recently-used.
func (c *Cache[T]) lookup(raw []byte) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[string(raw)]
	if !ok {
		var zero T
		return zero, false
	}
	c.moveToBack(e)
	return e.val, true
}

// store inserts v under raw, evicting the least recently used entry when the
// cap binds. A racing store for the same blob keeps the entry already there:
// purity makes the two values interchangeable, so the older node is the one
// concurrent readers may already hold.
func (c *Cache[T]) store(raw []byte, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[string(raw)]; ok {
		c.moveToBack(e)
		return
	}
	for len(c.m) >= maxEntries && c.head != nil {
		c.remove(c.head)
	}
	e := &entry[T]{val: v, key: string(raw)}
	c.m[e.key] = e
	c.pushBack(e)
}

// remove unlinks e from the recency queue and deletes its map slot.
func (c *Cache[T]) remove(e *entry[T]) {
	c.unlink(e)
	delete(c.m, e.key)
}

// moveToBack promotes e to most-recently-used. Already-at-the-tail is the
// steady state (one hot config re-read by every line of its channel's chat),
// so it short-circuits before touching any pointer.
func (c *Cache[T]) moveToBack(e *entry[T]) {
	if c.tail == e {
		return
	}
	c.unlink(e)
	c.pushBack(e)
}

// pushBack appends e at the most-recently-used end of the queue.
func (c *Cache[T]) pushBack(e *entry[T]) {
	e.prev, e.next = c.tail, nil
	if c.tail != nil {
		c.tail.next = e
	} else {
		c.head = e
	}
	c.tail = e
}

// unlink detaches e from the recency queue, fixing up head/tail.
func (c *Cache[T]) unlink(e *entry[T]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

// len reports the entry count; tests assert the cap holds.
func (c *Cache[T]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}
