// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"sync"
	"time"
)

// Verdict is the checker's classification of one host: the only two states the
// gate can act on. "Unknown" is not a state — it is the absence of an entry,
// and it always means ActionNone upstream.
type Verdict uint8

const (
	// Clean means every oracle cleared the host as of its last check.
	Clean Verdict = iota
	// Bad means at least one oracle flagged the host (feed, floor term, or
	// security-resolver block).
	Bad
)

func (v Verdict) String() string {
	if v == Bad {
		return "bad"
	}
	return "clean"
}

// Retention constants. Measured intuition, recorded so they can be re-argued:
//
//   - badTTL 24h: threat feeds rotate faster than a day, but flipping bad ->
//     clean is the dangerous direction (a still-live scam domain that starts
//     passing again). A day keeps false-ban risk bounded while domains die on
//     their own; ops lift a wrong ban by restarting with the feed entry gone.
//   - cleanTTL 6h: phishing infrastructure churns in hours, and a stale Clean
//     is exactly the miss this layer exists to close. Re-checking six-hour-old
//     clears costs one DoH query per host per shift.
//   - shortTTL 1h on shortener-keyed entries: bit.ly/abc's destination is
//     mutable server-side, so a cached expansion goes stale fastest of all.
//   - errCooldown 5m: an oracle outage must not turn into a hot retry loop
//     (every chat line re-enqueueing the same failing host), but five minutes
//     bounds how long a blip delays coverage.
const (
	badTTL      = 24 * time.Hour
	cleanTTL    = 6 * time.Hour
	shortTTL    = time.Hour
	errCooldown = 5 * time.Minute
)

// maxEntries caps the cache. Fleet-wide distinct chat hosts measure in the low
// thousands per day across all channels, so 16384 holds days of tail traffic;
// the cap exists because the alternative to a bound is unbounded growth from
// one flood of random hosts. When the cap bites, expired entries sweep first
// and the live entry closest to lapsing evicts next: chat host popularity is
// heavily Zipf, so an evicted hot host re-enqueues off its very next mention
// and re-resolves within seconds - eviction self-heals at the cost of one
// lookup, which no lock-free scheme would remove either.
const maxEntries = 16384

// ttlClass indexes the per-class expiry queues. One queue PER RETENTION CLASS
// is the whole trick behind O(1) sweeping: a single insertion-ordered queue is
// NOT in expiry order here (a 1h shortener written after a 24h Bad entry lapses
// first), but WITHIN a class every entry gets the same TTL off a monotone
// clock, so insertion order IS expiry order and the head is always the next
// entry to lapse. Three lists cost three head/tail pointer pairs; keeping the
// whole cache in one expiry-sorted list would need an ordered insert.
type ttlClass uint8

const (
	classShort ttlClass = iota // shortener tokens, shortTTL
	classClean                 // clean hosts, cleanTTL
	classBad                   // flagged hosts, badTTL
)

const numTTLClasses = 3

// entry is an intrusive list node as well as a cached verdict: prev/next thread
// it onto its class queue, and key lets the sweeper delete the map slot without
// a reverse lookup. Intrusive rather than a separate index because the whole
// point is that unlinking on eviction, on re-put and on lazy expiry-at-read is
// O(1) with no second map to keep in step.
type entry struct {
	v    Verdict
	exp  int64 // unix nanos; lazily deleted on read past this
	cls  ttlClass
	key  string
	prev *entry
	next *entry
}

// cache maps cache keys (hosts, folded hosts, or lowercased shortener tokens)
// to verdicts. A plain mutex guards it: reads are map lookups (~50ns) on a path
// that already accepted a dot pre-filter, writes come only from the checker's
// worker goroutines, and contention at chat scale never registers next to the
// network calls those workers just made.
//
// 2026-09-03: the sweep used to range the ENTIRE 16384-slot map under this
// mutex every time the cap bit, which parks every concurrent get() - and get()
// sits on the automod deep path. The per-class queues below make the sweep pop
// off three heads instead, so the lock is held for the number of entries that
// actually lapsed rather than for the size of the cache. The cost is two
// pointers plus the key string per entry (~40 bytes x 16384 ~= 650KB worst
// case) and one heap allocation per put; put is the worker-goroutine path that
// just finished a DNS round trip, so that allocation is free in context.
type cache struct {
	mu   sync.Mutex
	m    map[string]*entry
	head [numTTLClasses]*entry
	tail [numTTLClasses]*entry
}

func newCache() *cache {
	return &cache{m: make(map[string]*entry, 256)}
}

// get returns the cached verdict for key, dropping expired entries lazily.
// Expiry-on-read keeps a sweeper goroutine out of the design: chat revisits
// hosts far more often than TTLs lapse, so the lazy delete IS the sweep.
func (c *cache) get(key string) (Verdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return Clean, false
	}
	if nowNanos() > e.exp {
		c.remove(e)
		return Clean, false
	}
	return e.v, true
}

// put stores a verdict under key with the retention class implied by v and
// short. It sweeps when the hard cap is reached rather than every insert.
func (c *cache) put(key string, v Verdict, short bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := nowNanos()
	if len(c.m) >= maxEntries {
		c.sweepLocked(now)
	}
	if old, ok := c.m[key]; ok {
		c.remove(old) // re-put: the old node carries the old class and expiry
	}
	cls, ttl := retention(v, short)
	e := &entry{v: v, exp: now + int64(ttl), cls: cls, key: key}
	c.m[key] = e
	c.pushBack(e)
}

// retention maps a verdict onto its queue and TTL (see the retention block).
func retention(v Verdict, short bool) (ttlClass, time.Duration) {
	switch {
	case short:
		return classShort, shortTTL
	case v == Bad:
		return classBad, badTTL
	default:
		return classClean, cleanTTL
	}
}

// sweepLocked makes room under the hard cap: expired entries go first, and
// only if the cap still binds do live entries go too (see maxEntries for why
// dropping a live entry is acceptable here).
func (c *cache) sweepLocked(now int64) {
	c.dropExpired(now)
	for len(c.m) >= maxEntries {
		e := c.earliestHead()
		if e == nil {
			return
		}
		c.remove(e)
	}
}

// dropExpired pops lapsed entries off each class head. O(1) amortized per
// dropped entry: the head is that class's oldest, so each loop stops at the
// first live node instead of touching entries that are nowhere near their TTL.
func (c *cache) dropExpired(now int64) {
	for cls := range c.head {
		for e := c.head[cls]; e != nil && now > e.exp; e = c.head[cls] {
			c.remove(e)
		}
	}
}

// earliestHead returns the live entry closest to lapsing across all classes,
// or nil when the cache is empty. Three comparisons, not a scan of the map:
// each class head is already that class's minimum expiry.
func (c *cache) earliestHead() *entry {
	var best *entry
	for _, e := range c.head {
		best = earlier(best, e)
	}
	return best
}

// earlier folds two candidate heads into the one that lapses first, treating
// nil as "no candidate". Split out of earliestHead's loop so the nil-vs-nil and
// the expiry comparison are separate one-clause tests instead of one conditional
// mixing both: the folded form reads the same and costs the same, and the
// combined test was the kind of clause pile-up that reviews keep catching.
func earlier(best, e *entry) *entry {
	if e == nil {
		return best
	}
	if best == nil {
		return e
	}
	if e.exp < best.exp {
		return e
	}
	return best
}

// remove unlinks e from its class queue and deletes its map slot.
func (c *cache) remove(e *entry) {
	c.unlink(e)
	delete(c.m, e.key)
}

// pushBack appends e to the tail of its class queue, where its expiry is by
// construction the latest in that class.
func (c *cache) pushBack(e *entry) {
	cls := e.cls
	e.prev, e.next = c.tail[cls], nil
	if c.tail[cls] != nil {
		c.tail[cls].next = e
	} else {
		c.head[cls] = e
	}
	c.tail[cls] = e
}

// unlink detaches e from its class queue, fixing up the head/tail pointers.
func (c *cache) unlink(e *entry) {
	cls := e.cls
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head[cls] = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail[cls] = e.prev
	}
	e.prev, e.next = nil, nil
}

// nowNanos is split out so tests can pin time instead of sleeping.
var nowNanos = func() int64 { return time.Now().UnixNano() }
