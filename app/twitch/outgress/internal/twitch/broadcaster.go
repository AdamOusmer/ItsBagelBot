// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"sync"
	"time"
)

// The token cache is bounded so a long tail of one-off broadcaster jobs cannot
// grow the map for the life of the process. Evicting a Source is safe: the
// refresh token lives in the users service, so a rebuilt Source resumes from
// the stored grant on its next renewal.
const (
	maxBroadcasterSources = 2048
	sourceIdleTTL         = time.Hour
)

type sourceEntry struct {
	source   *Source
	lastUsed time.Time
}

// BroadcasterTokens lazily builds and caches one user-token Source per
// broadcaster id, so "send as the broadcaster" jobs reuse a single refreshing
// token per channel instead of re-minting on every message. The built Source
// loads the channel's refresh token from the users service on each renewal, so
// a channel that authorizes later starts working without a restart (and one
// that never authorized simply errors at send time).
type BroadcasterTokens struct {
	mu    sync.Mutex
	cache map[string]*sourceEntry
	build func(broadcasterID string) *Source
}

func NewBroadcasterTokens(build func(broadcasterID string) *Source) *BroadcasterTokens {
	return &BroadcasterTokens{cache: make(map[string]*sourceEntry), build: build}
}

// Get returns the cached Source for a broadcaster, building it on first use.
func (b *BroadcasterTokens) Get(broadcasterID string) *Source {
	if b == nil || broadcasterID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if e, ok := b.cache[broadcasterID]; ok {
		e.lastUsed = now
		return e.source
	}
	if len(b.cache) >= maxBroadcasterSources {
		b.evictLocked(now)
	}
	s := b.build(broadcasterID)
	b.cache[broadcasterID] = &sourceEntry{source: s, lastUsed: now}
	return s
}

// evictLocked drops every source idle past sourceIdleTTL, falling back to the
// least recently used one so an insert never grows the map past the cap. A
// caller already holding an evicted *Source keeps using it safely; only the
// cache slot is released.
func (b *BroadcasterTokens) evictLocked(now time.Time) {
	var oldestID string
	var oldestUse time.Time
	for id, e := range b.cache {
		if now.Sub(e.lastUsed) >= sourceIdleTTL {
			delete(b.cache, id)
			continue
		}
		if oldestID == "" || e.lastUsed.Before(oldestUse) {
			oldestID, oldestUse = id, e.lastUsed
		}
	}
	if len(b.cache) >= maxBroadcasterSources && oldestID != "" {
		delete(b.cache, oldestID)
	}
}

// broadcasterSweepInterval is how often StartRefreshSweep walks the cache
// looking for entries due for renewal. It only needs to be finer than
// refreshMargin (5m) to reliably catch that window, the same requirement
// token.go's backgroundRefreshInterval satisfies with a 1-minute tick for a
// single Source. This sweep instead walks up to maxBroadcasterSources
// (2048) entries per pass, so the interval is deliberately coarser: each
// entry's due-check (Source.cached) is a lock-protected local read, and on
// a real fleet the vast majority of cached broadcasters are not within 5
// minutes of expiry on any given pass, so walking all 2048 is cheap
// regardless of tick rate -- the cost that actually scales is the occasional
// refresh a pass finds due, and that is bounded by how many entries fall
// inside the 5-minute window, not by how often the ticker fires.
//
// Safety factor: refreshMargin (5m) / broadcasterSweepInterval (2m) = 2.5x.
// A single fully skipped pass still leaves the next one landing at the
// 4-minute mark, inside the 5-minute margin. Two consecutive skips (6
// minutes) would exceed it -- at that point the still-live sourceEntry is
// not yet expired (refreshMargin is a renewal window, not the token's
// actual lifetime), and any chat send in between falls back to Token's
// lazy refresh path (same backstop backgroundRefreshInterval's doc
// describes), so a missed sweep degrades to paying one mint's latency on a
// chat send rather than serving an expired token. 2 minutes is a judgement
// call, not a measurement -- unlike tokenClientTimeout or leaseWaitInterval
// (token.go), no load test backs this number, only the safety-factor
// arithmetic above.
const broadcasterSweepInterval = 2 * time.Minute

// StartRefreshSweep renews cached broadcaster tokens proactively, off the
// chat send path, until ctx is cancelled.
//
// WHY THIS EXISTS, AND WHY IT DIDN'T BEFORE: broadcasterTokens
// (app/twitch/outgress/main.go) originally skipped background refresh here on the
// reasoning that a cached Source would almost never survive to its ~4h
// refresh window before sourceIdleTTL (1h) evicted it. That reasoning
// ignored that Get (above) sets lastUsed on every cache hit: a broadcaster
// that is actively streaming hits its Source on every chat message, so it
// is never idle and never evicted, and DOES live long enough to reach the
// ~4h expiry while live. Before this fix, that renewal happened lazily
// inside Source.Token on whatever chat message first observed the expiry --
// a ~320ms mint (measured 2026-08-20, see tokenIdleConnTimeout's doc in
// token.go) landing mid-stream. The exclusion only ever protected idle
// broadcasters, which don't matter: nothing is being sent for a slow
// renewal to land on top of.
//
// ONE SWEEPER GOROUTINE, NOT ONE PER SOURCE: maxBroadcasterSources is 2048,
// so a per-Source ticker (the shape token.go's StartBackgroundRefresh uses
// for the two long-lived app/bot sources) would mean up to 2048 tickers,
// plus eviction-tied cancellation plumbing to stop each one when its Source
// is evicted -- otherwise every eviction leaks a goroutine. A single ticker
// that walks the cache gets eviction handling for free: an entry dropped
// from the map is simply absent from the next pass's snapshot, so it is no
// longer swept. Nothing to cancel.
func (b *BroadcasterTokens) StartRefreshSweep(ctx context.Context) {
	go b.runRefreshSweep(ctx)
}

func (b *BroadcasterTokens) runRefreshSweep(ctx context.Context) {
	ticker := time.NewTicker(broadcasterSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.sweepOnce(ctx)
		}
	}
}

// broadcasterSweepBudget bounds how long a single sweepOnce pass may run
// before it stops early and leaves whatever entries it has not yet reached
// for the next pass. Set to broadcasterSweepInterval itself: a pass that
// hits the budget costs at most one full interval, which is exactly
// equivalent to "one skipped pass" in broadcasterSweepInterval's own
// safety-factor comment above (2.5x margin against refreshMargin, and a
// single skip still lands well inside it). Without this bound, a pass
// degraded by a slow Twitch or users-service response has no stated
// worst-case duration at all -- every other constant in this lease/refresh
// path does. Entries not reached before the budget fires are not lost:
// Source.refreshIfDue still lets a live caller renew lazily on its own send
// in the meantime, and the next pass picks up anything still due.
const broadcasterSweepBudget = broadcasterSweepInterval

// sweepOnce snapshots the cached sources under mu, then refreshes each one
// only AFTER the lock is released, bounded by broadcasterSweepBudget so one
// slow pass can never run past the next scheduled tick. This ordering is
// the entire point of this function: Source.refreshIfDue performs NATS RPC
// and HTTP I/O (through refresh -> mintOrAdopt, token.go), potentially for
// every one of up to 2048 entries in a single pass. Holding b.mu across that
// would block Get for as long as the whole pass takes -- and Get sits on the
// send path of every broadcaster-identity message the process handles, so a
// lock held across sweep I/O would stall chat sends fleet-wide, exactly the
// outcome a background refresher exists to avoid on a per-message basis.
// Snapshotting under the lock and refreshing after releasing it keeps Get's
// critical section a plain map read, unaffected by how many entries this
// pass refreshes or how long id.twitch.tv takes to answer.
func (b *BroadcasterTokens) sweepOnce(ctx context.Context) {
	budgetCtx, cancel := context.WithTimeout(ctx, broadcasterSweepBudget)
	defer cancel()
	for _, s := range b.snapshotSources() {
		if budgetCtx.Err() != nil {
			return
		}
		s.refreshIfDue(budgetCtx)
	}
}

// snapshotSources copies out the cached *Source pointers under mu and
// returns immediately, deliberately not touching lastUsed on any entry:
// sweeping must not look like use, or an otherwise-idle broadcaster that
// merely keeps getting swept would never cross sourceIdleTTL and would sit
// in the cache forever, defeating the eviction the cap depends on.
func (b *BroadcasterTokens) snapshotSources() []*Source {
	b.mu.Lock()
	defer b.mu.Unlock()
	sources := make([]*Source, 0, len(b.cache))
	for _, e := range b.cache {
		sources = append(sources, e.source)
	}
	return sources
}
