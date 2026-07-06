package core

import (
	"context"
	"sync"
	"time"

	"github.com/bytedance/sonic"
)

// Byte-flow caching: the pass-through endpoints (urchin, hypixel) shape their
// reply once on fetch and cache the READY-TO-SEND wire bytes. A cache hit then
// answers with zero JSON work — no envelope unmarshal, no reply re-marshal —
// the stored payload is sliced out of the entry and handed straight to the
// NATS responder. This mirrors sesame's hot-path discipline (pooled buffers,
// sonic, no allocation above the transport floor).
//
// Entry format: {"gw1":<reply bytes>} — a fixed prefix marker plus the raw
// reply. The marker is the format guard: an entry without it (legacy format,
// foreign writer, shape drift after a version bump) reads as poison and is
// refetched, never served as a zero-value reply. Bumping the format is
// renaming the marker. The whole entry stays valid JSON so operators can read
// it in valkey-cli.
var (
	entryPrefix = []byte(`{"gw1":`)
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

// unwrapEntry extracts the reply payload from one stored entry, or reports a
// format mismatch. The returned slice aliases b.
func unwrapEntry(b []byte) ([]byte, bool) {
	if len(b) < len(entryPrefix)+1 || b[len(b)-1] != entrySuffix {
		return nil, false
	}
	for i := range entryPrefix {
		if b[i] != entryPrefix[i] {
			return nil, false
		}
	}
	return b[len(entryPrefix) : len(b)-1], true
}

// CachedBytes returns the ready-to-send reply bytes under key, or runs build to
// produce them. build returns the reply bytes and the TTL to store them for;
// a TTL of zero (or less) answers without caching — that is how a rate-limit
// denial stays friendly but is retried on the very next request. Negative
// replies (player not found) are ordinary replies with a short TTL: the
// provider shapes them, so a hit on a negative costs the same zero JSON work
// as a hit on a success.
//
// Concurrent misses for one key collapse through singleflight. A Store error
// degrades to a direct build rather than failing the lookup.
func CachedBytes(ctx context.Context, c *Cache, key string, build func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	if b, ok, err := c.store.Get(ctx, key); err == nil && ok {
		if payload, valid := unwrapEntry(b); valid {
			return payload, nil
		}
		// Poison entry (legacy or foreign format): drop it and refetch.
		_ = c.store.Del(ctx, key)
	}

	res, err, _ := c.sf.Do(key, func() (any, error) {
		// A previous flight may have filled the key while we queued.
		if b, ok, gerr := c.store.Get(ctx, key); gerr == nil && ok {
			if payload, valid := unwrapEntry(b); valid {
				return payload, nil
			}
		}

		payload, ttl, berr := build(ctx)
		if berr != nil {
			return nil, berr
		}
		if ttl > 0 {
			bufp := entryBufPool.Get().(*[]byte)
			buf := (*bufp)[:0]
			buf = append(buf, entryPrefix...)
			buf = append(buf, payload...)
			buf = append(buf, entrySuffix)
			_ = c.store.Set(ctx, key, buf, ttl)
			*bufp = buf[:0]
			entryBufPool.Put(bufp)
		}
		return payload, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// MarshalReply renders one reply value for the wire (and for CachedBytes
// storage) through sonic, the fleet's hot-path JSON codec.
func MarshalReply(v any) ([]byte, error) { return sonic.Marshal(v) }
