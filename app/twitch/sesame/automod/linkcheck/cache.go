// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"sync"
	"time"
)

type Verdict uint8

const (
	Clean Verdict = iota
	Bad
)

func (v Verdict) String() string {
	if v == Bad {
		return "bad"
	}
	return "clean"
}

const (
	badTTL      = 24 * time.Hour
	cleanTTL    = 6 * time.Hour
	shortTTL    = time.Hour
	errCooldown = 5 * time.Minute
)

const maxEntries = 16384

type ttlClass uint8

const (
	classShort ttlClass = iota
	classClean
	classBad
)

const numTTLClasses = 3

type entry struct {
	v        Verdict
	expNanos int64
	cls      ttlClass
	key      string
	prev     *entry
	next     *entry
}

type cache struct {
	mu   sync.Mutex
	m    map[string]*entry
	head [numTTLClasses]*entry
	tail [numTTLClasses]*entry
}

func newCache() *cache {
	return &cache{m: make(map[string]*entry, 256)}
}

func (c *cache) get(key string) (Verdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return Clean, false
	}
	if nowNanos() > e.expNanos {
		c.remove(e)
		return Clean, false
	}
	return e.v, true
}

func (c *cache) put(key string, v Verdict, short bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := nowNanos()
	if old, ok := c.m[key]; ok {
		c.remove(old)
	}
	if len(c.m) >= maxEntries {
		c.sweepLocked(now)
	}
	cls, ttl := retention(v, short)
	e := &entry{v: v, expNanos: now + int64(ttl), cls: cls, key: key}
	c.m[key] = e
	c.pushBack(e)
}

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

func (c *cache) dropExpired(now int64) {
	for cls := range c.head {
		for e := c.head[cls]; e != nil && now > e.expNanos; e = c.head[cls] {
			c.remove(e)
		}
	}
}

func (c *cache) earliestHead() *entry {
	var best *entry
	for _, e := range c.head {
		best = earlier(best, e)
	}
	return best
}

func earlier(best, e *entry) *entry {
	if e == nil {
		return best
	}
	if best == nil {
		return e
	}
	if e.expNanos < best.expNanos {
		return e
	}
	return best
}

func (c *cache) remove(e *entry) {
	c.unlink(e)
	delete(c.m, e.key)
}

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

var nowNanos = func() int64 { return time.Now().UnixNano() }
