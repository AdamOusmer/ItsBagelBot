// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"time"
)

// Byte-flow caching with stale-while-revalidate: the pass-through endpoints
// (urchin, hypixel, govee) shape their reply once on fetch and cache the
// READY-TO-SEND wire bytes. A hit answers with zero JSON work — no envelope
// unmarshal, no reply re-marshal — the stored payload is sliced out of the
// entry and handed straight to the NATS responder. This mirrors sesame's
// hot-path discipline (pooled buffers, sonic, no allocation above the transport
// floor).
//
// SWR: an entry is stored with a fresh window (the build's TTL) and physically
// retained for twice that. A read inside the fresh window returns the payload
// as-is; a read in the stale tail returns the payload immediately AND kicks a
// single background refresh, so the slow upstream (a Govee cloud round trip) is
// almost never on a caller's critical path — only the very first, cold fetch is.
//
// The stored record's format, and the reader and writer that agree on it, live
// in entry.go; this file is the policy that decides when to read, when to serve
// stale, and who pays for a refill.

// swrRefreshTimeout bounds a background revalidation's own context (the request
// that triggered it has already been answered from the stale value).
const swrRefreshTimeout = 15 * time.Second

// CachedBytes returns the ready-to-send reply bytes under key, or runs build to
// produce them. build returns the reply bytes and the fresh-window TTL to store
// them for; a TTL of zero (or less) answers without caching — that is how a
// rate-limit denial stays friendly but is retried on the very next request.
// Negative replies (player not found) are ordinary replies with a short TTL: the
// provider shapes them, so a hit on a negative costs the same zero JSON work
// as a hit on a success.
//
// A fresh hit returns as-is; a stale hit returns the stored bytes and revalidates
// in the background. Concurrent misses for one key collapse through singleflight.
// A Store error degrades to a direct build rather than failing the lookup.
//
// admit runs ONCE PER CALLER that reaches the miss path, before that caller joins
// the flight, and its error is that caller's alone. build runs once for the whole
// flight. The split exists because those two things answer different questions: a
// flight is "who pays for the upstream call", admission is "may THIS request spend
// budget", and collapsing them let one caller's answer stand in for another's. The
// concrete failure was the premium reserve — a standard caller that won the flight
// ran the budget check for everyone joined to it, so a drained standard bucket
// denied a premium caller the 25% reserve premium is entitled to, and reversing the
// roles let a standard caller spend on the premium lane's ticket. Admission sits
// after the fresh-hit check on purpose: a hit costs no upstream call, so it must
// cost no budget either, or the buckets would meter chat volume instead of upstream
// spend. A nil admit means the endpoint has no per-caller budget.
func CachedBytes(ctx context.Context, c *Cache, key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	if payload, stale, ok := c.readBytes(ctx, key); ok {
		if stale {
			// Still retained past its fresh window: serve it now, revalidate behind
			// the scenes.
			c.refreshBytes(key, admit, build)
		}
		return payload, nil
	}

	if err := spend(ctx, admit); err != nil {
		return nil, err
	}

	res, err, _ := c.sf.Do(key, func() (any, error) {
		// A previous flight may have filled the key while we queued.
		if b, ok, gerr := c.store.Get(ctx, key); gerr == nil && ok {
			if _, payload, valid := unwrapEntry(b); valid {
				return payload, nil
			}
		}

		payload, ttl, berr := build(ctx)
		if berr != nil {
			return nil, berr
		}
		c.storeEntry(ctx, key, payload, ttl)
		return payload, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// readBytes reads one stored entry, reporting the payload and whether it has
// passed its fresh window. ok is false when there is nothing usable: no entry, a
// store error, or an entry whose format does not parse — the last of which is
// dropped here so the caller's refetch replaces it rather than serving a
// zero-value reply out of a legacy or foreign record.
func (c *Cache) readBytes(ctx context.Context, key string) (payload []byte, stale, ok bool) {
	b, found, err := c.store.Get(ctx, key)
	if err != nil || !found {
		return nil, false, false
	}
	fresh, payload, valid := unwrapEntry(b)
	if !valid {
		_ = c.store.Del(ctx, key)
		return nil, false, false
	}
	return payload, time.Now().UnixMilli() >= fresh, true
}

// spend runs one caller's budget check, treating an undeclared budget as free.
func spend(ctx context.Context, admit func(context.Context) error) error {
	if admit == nil {
		return nil
	}
	return admit(ctx)
}

// refreshBytes revalidates one stale key in the background. Deduplication is
// fleet-wide: the refresh is gated on a SET NX EX claim in the shared store, so
// exactly ONE replica refreshes a stale key no matter how a burst spreads
// across the queue group — without the claim each pod would fire its own
// refresh and one stale key would cost one upstream call per replica. The
// pod-local c.refreshing map is only a cheap pre-filter that keeps one pod from
// stacking goroutines on a hot key; the claim in Valkey is the authority.
//
// The claim expires on its own (never deleted early), so refresh attempts for
// one key are floored to one per swrRefreshTimeout fleet-wide — a deliberate
// backoff that shields the upstream. A successful refresh rewrites the entry
// fresh, so nothing re-triggers anyway; a failed refresh is swallowed and the
// stale entry keeps serving until its physical TTL, so an upstream blip
// degrades to slightly-old data rather than an error.
//
// The refresh spends budget through admit, exactly like a foreground miss, and it
// does so AFTER winning the claim so only the one replica that will actually call
// the upstream pays for it. Skipping admission here would let revalidation traffic
// bypass the buckets entirely: the whole point of moving the budget check out of
// build and onto the caller (see CachedBytes) is that build no longer carries one,
// so a refresh that did not admit would be an unmetered upstream call. A denial
// leaves the stale entry serving, which is the same outcome as any other failed
// refresh. The lane charged is that of the caller whose stale read triggered this;
// that caller was served for free from the stale entry, so the one call the refresh
// makes is fairly billed to it.
func (c *Cache) refreshBytes(key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) {
	if _, busy := c.refreshing.LoadOrStore(key, struct{}{}); busy {
		return
	}
	go func() {
		defer c.refreshing.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), swrRefreshTimeout)
		defer cancel()
		if won, err := c.store.SetNX(ctx, key+":swr", []byte("1"), swrRefreshTimeout); err != nil || !won {
			// Lost claim: another replica is refreshing. Claim error: leave the
			// stale entry serving; the next read retries the claim.
			return
		}
		if err := spend(ctx, admit); err != nil {
			return
		}
		_, _, _ = c.sf.Do(key, func() (any, error) {
			payload, ttl, err := build(ctx)
			if err != nil {
				return nil, err
			}
			c.storeEntry(ctx, key, payload, ttl)
			return payload, nil
		})
	}()
}

// MarshalReply renders one reply value for the wire (and for CachedBytes
// storage) through sonic, the fleet's hot-path JSON codec.
func MarshalReply(v any) ([]byte, error) { return codec.Marshal(v) }
