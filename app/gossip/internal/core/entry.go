// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package core

import (
	"bytes"
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
// The codec lives apart from the caching policy in bytes.go because it is the one
// thing a stored record must agree on across deploys: unwrapEntry and storeEntry
// are the two halves of that agreement and only make sense read together.
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
// reports a format mismatch. The returned payload slice aliases b: every step
// below slices, none of them copy, so the hit path stays allocation-free (see
// BenchmarkCachedBytesHit).
func unwrapEntry(b []byte) (int64, []byte, bool) {
	body, ok := entryBody(b)
	if !ok {
		return 0, nil, false
	}
	fresh, rest, ok := scanStamp(body)
	if !ok {
		return 0, nil, false
	}
	payload, ok := cutPrefix(rest, entryMid)
	if !ok || len(payload) == 0 {
		return 0, nil, false
	}
	return fresh, payload, true
}

// entryBody strips the format marker and the closing brace, returning what sits
// between them. A record that does not carry the marker is not ours to read.
func entryBody(b []byte) ([]byte, bool) {
	if len(b) < len(entryPrefix)+len(entryMid)+1 || b[len(b)-1] != entrySuffix {
		return nil, false
	}
	if !bytes.HasPrefix(b, entryPrefix) {
		return nil, false
	}
	return b[len(entryPrefix) : len(b)-1], true
}

// scanStamp reads the leading fresh-until digits and returns them with whatever
// follows. It parses inline rather than through a JSON number so the hit path
// does no unmarshal; an entry with no digits at all is a format mismatch.
func scanStamp(b []byte) (stamp int64, rest []byte, ok bool) {
	i := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		stamp = stamp*10 + int64(b[i]-'0')
		i++
	}
	if i == 0 {
		return 0, nil, false
	}
	return stamp, b[i:], true
}

// cutPrefix returns what follows sep when b starts with it.
func cutPrefix(b, sep []byte) ([]byte, bool) {
	if !bytes.HasPrefix(b, sep) {
		return nil, false
	}
	return b[len(sep):], true
}

// storeEntry writes payload with a fresh window of ttl and a physical retention
// of 2*ttl, so the entry outlives its fresh window into a stale tail where it is
// served while a background refresh runs. ttl<=0 is not cached (a friendly
// rate-limit denial must retry on the next request, never pin).
//
// It hangs off the Cache, like readBytes: the store it writes through is the
// receiver's, not a parameter a caller could pass a different one for.
func (c *Cache) storeEntry(ctx context.Context, key string, payload []byte, ttl time.Duration) {
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
