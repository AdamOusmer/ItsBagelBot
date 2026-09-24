// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"time"
)

// Change the entry format only by renaming this marker, or mixed-version replicas misread entries.
var (
	entryPrefix = []byte(`{"gw2":`)
	entryMid    = []byte(`,"p":`)
	entrySuffix = byte('}')
)

var entryBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

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

func entryBody(b []byte) ([]byte, bool) {
	if len(b) < len(entryPrefix)+len(entryMid)+1 || b[len(b)-1] != entrySuffix {
		return nil, false
	}
	if !bytes.HasPrefix(b, entryPrefix) {
		return nil, false
	}
	return b[len(entryPrefix) : len(b)-1], true
}

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

func cutPrefix(b, sep []byte) ([]byte, bool) {
	if !bytes.HasPrefix(b, sep) {
		return nil, false
	}
	return b[len(sep):], true
}

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
