package core

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/bytedance/sonic"
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
// Entry format: {"gw2":<freshUntilUnixMs>,"p":<reply bytes>} — a fixed prefix
// marker, the fresh-until stamp, then the raw reply. The marker is the format
// guard: an entry without it (legacy {"gw1":…}, foreign writer, shape drift
// after a version bump) reads as poison and is refetched, never served as a
// zero-value reply. Bumping the format is renaming the marker. The whole entry
// stays valid JSON so operators can read it in valkey-cli.
var (
	entryPrefix = []byte(`{"gw2":`)
	entryMid    = []byte(`,"p":`)
	entrySuffix = byte('}')
)

// swrRefreshTimeout bounds a background revalidation's own context (the request
// that triggered it has already been answered from the stale value).
const swrRefreshTimeout = 15 * time.Second

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
func CachedBytes(ctx context.Context, c *Cache, key string, build func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	if b, ok, err := c.store.Get(ctx, key); err == nil && ok {
		if fresh, payload, valid := unwrapEntry(b); valid {
			if time.Now().UnixMilli() < fresh {
				return payload, nil
			}
			// Stale but still retained: serve it now, revalidate behind the scenes.
			c.refreshBytes(key, build)
			return payload, nil
		}
		// Poison entry (legacy or foreign format): drop it and refetch.
		_ = c.store.Del(ctx, key)
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
		storeEntry(ctx, c, key, payload, ttl)
		return payload, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// storeEntry writes payload with a fresh window of ttl and a physical retention
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

// refreshBytes revalidates one stale key in the background, deduped via
// c.refreshing so only one refresh runs per key. It uses a detached context (the
// triggering request was already answered from the stale value) and
// single-flights with any concurrent blocking miss. A failed refresh is
// swallowed and leaves the stale entry to expire on its physical TTL, so an
// upstream blip degrades to slightly-old data rather than an error.
func (c *Cache) refreshBytes(key string, build func(context.Context) ([]byte, time.Duration, error)) {
	if _, busy := c.refreshing.LoadOrStore(key, struct{}{}); busy {
		return
	}
	go func() {
		defer c.refreshing.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), swrRefreshTimeout)
		defer cancel()
		_, _, _ = c.sf.Do(key, func() (any, error) {
			payload, ttl, err := build(ctx)
			if err != nil {
				return nil, err
			}
			storeEntry(ctx, c, key, payload, ttl)
			return payload, nil
		})
	}()
}

// MarshalReply renders one reply value for the wire (and for CachedBytes
// storage) through sonic, the fleet's hot-path JSON codec.
func MarshalReply(v any) ([]byte, error) { return sonic.Marshal(v) }
