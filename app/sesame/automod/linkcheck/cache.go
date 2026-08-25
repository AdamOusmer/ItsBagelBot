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
// and live entries evict arbitrarily (Go map range order): chat host popularity
// is heavily Zipf, so an evicted hot host re-enqueues off its very next mention
// and re-resolves within seconds — eviction self-heals at the cost of one
// lookup, which no lock-free scheme would remove either.
const maxEntries = 16384

type entry struct {
	v   Verdict
	exp int64 // unix nanos; lazily deleted on read past this
}

// cache maps cache keys (hosts, folded hosts, or lowercased shortener tokens)
// to verdicts. A plain mutex guards it: reads are map lookups (~50ns) on a path
// that already accepted a dot pre-filter, writes come only from the checker's
// worker goroutines, and contention at chat scale never registers next to the
// network calls those workers just made.
type cache struct {
	mu sync.Mutex
	m  map[string]entry
}

func newCache() *cache {
	return &cache{m: make(map[string]entry, 256)}
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
		delete(c.m, key)
		return Clean, false
	}
	return e.v, true
}

// put stores a verdict under key with the retention class implied by v and
// short. It sweeps when the hard cap is reached rather than every insert.
func (c *cache) put(key string, v Verdict, short bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= maxEntries {
		c.sweepLocked()
	}
	ttl := cleanTTL
	switch {
	case short:
		ttl = shortTTL
	case v == Bad:
		ttl = badTTL
	}
	c.m[key] = entry{v: v, exp: nowNanos() + int64(ttl)}
}

// sweepLocked makes room under the hard cap: expired entries go first, and
// only if the cap still binds do arbitrary live entries go too (see
// maxEntries for why arbitrary is acceptable here).
func (c *cache) sweepLocked() {
	c.dropExpired(nowNanos())
	if len(c.m) >= maxEntries {
		c.evictArbitrary()
	}
}

// dropExpired deletes every entry past its TTL.
func (c *cache) dropExpired(now int64) {
	for k, e := range c.m {
		if now > e.exp {
			delete(c.m, k)
		}
	}
}

// evictArbitrary deletes live entries until the cap holds.
func (c *cache) evictArbitrary() {
	for k := range c.m {
		if len(c.m) < maxEntries {
			return
		}
		delete(c.m, k)
	}
}

// nowNanos is split out so tests can pin time instead of sleeping.
var nowNanos = func() int64 { return time.Now().UnixNano() }
