package core

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// The byte-flow entry format: {"gw2":<freshUntilUnixMs>,"p":<reply bytes>} — a
// fixed prefix marker, the fresh-until stamp, then the raw reply. The marker is
// the format guard: an entry without it (legacy {"gw1":…}, foreign writer, shape
// drift after a version bump) reads as poison and is refetched, never served as a
// zero-value reply. Bumping the format is renaming the marker. The whole entry
// stays valid JSON so operators can read it in valkey-cli.
//
// The codec lives apart from the caching policy in bytes.go because it is the
// one thing a stored record must agree on across deploys: the reader below and
// the writer at the bottom of this file are the two halves of that agreement,
// and they only make sense read together.
var (
	entryPrefix = []byte(`{"gw2":`)
	entryMid    = []byte(`,"p":`)
	entrySuffix = byte('}')
)

// entryBufPool recycles the scratch buffers entries are assembled in before
// the store write copies them onto the wire.
var entryBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

// unwrapEntry extracts (freshUntilUnixMs, payload) from one stored entry, or
// reports a format mismatch. The returned payload slice aliases b. It scans the
// fresh-until digits inline — no JSON unmarshal — so the hit path stays
// allocation-free.
func unwrapEntry(b []byte) (int64, []byte, bool) {
	if len(b) < len(entryPrefix)+len(entryMid)+1 || b[len(b)-1] != entrySuffix {
		return 0, nil, false
	}
	for i := range entryPrefix {
		if b[i] != entryPrefix[i] {
			return 0, nil, false
		}
	}
	i := len(entryPrefix)
	start := i
	var fresh int64
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		fresh = fresh*10 + int64(b[i]-'0')
		i++
	}
	if i == start || i+len(entryMid) > len(b) {
		return 0, nil, false
	}
	for j := range entryMid {
		if b[i+j] != entryMid[j] {
			return 0, nil, false
		}
	}
	payload := b[i+len(entryMid) : len(b)-1]
	if len(payload) == 0 {
		return 0, nil, false
	}
	return fresh, payload, true
}

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
// of 2*ttl, so the entry outlives its fresh window into a stale tail where it is
// served while a background refresh runs. ttl<=0 is not cached (a friendly
// rate-limit denial must retry on the next request, never pin).
func storeEntry(ctx context.Context, c *Cache, key string, payload []byte, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	freshMs := time.Now().Add(ttl).UnixMilli()
	bufp := entryBufPool.Get().(*[]byte)
	buf := (*bufp)[:0]
	buf = append(buf, entryPrefix...)
	buf = strconv.AppendInt(buf, freshMs, 10)
	buf = append(buf, entryMid...)
	buf = append(buf, payload...)
	buf = append(buf, entrySuffix)
	_ = c.store.Set(ctx, key, buf, 2*ttl)
	*bufp = buf[:0]
	entryBufPool.Put(bufp)
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
