// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package confcache

import "sync"

const maxEntries = 1024

type entry[T any] struct {
	val  T
	key  string
	prev *entry[T]
	next *entry[T]
}

// Get's values are shared across channels; callers must copy before mutating.
type Cache[T any] struct {
	mu   sync.Mutex
	m    map[string]*entry[T]
	head *entry[T]
	tail *entry[T]
}

func New[T any]() *Cache[T] {
	return &Cache[T]{m: make(map[string]*entry[T], 64)}
}

func (c *Cache[T]) Get(raw []byte, parse func([]byte) T) T {
	if len(raw) == 0 {
		return parse(raw)
	}
	if v, ok := c.lookup(raw); ok {
		return v
	}
	v := parse(raw)
	c.store(raw, v)
	return v
}

func (c *Cache[T]) lookup(raw []byte) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[string(raw)]
	if !ok {
		var zero T
		return zero, false
	}
	c.moveToBack(e)
	return e.val, true
}

func (c *Cache[T]) store(raw []byte, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[string(raw)]; ok {
		c.moveToBack(e)
		return
	}
	for len(c.m) >= maxEntries && c.head != nil {
		c.remove(c.head)
	}
	e := &entry[T]{val: v, key: string(raw)}
	c.m[e.key] = e
	c.pushBack(e)
}

func (c *Cache[T]) remove(e *entry[T]) {
	c.unlink(e)
	delete(c.m, e.key)
}

func (c *Cache[T]) moveToBack(e *entry[T]) {
	if c.tail == e {
		return
	}
	c.unlink(e)
	c.pushBack(e)
}

func (c *Cache[T]) pushBack(e *entry[T]) {
	e.prev, e.next = c.tail, nil
	if c.tail != nil {
		c.tail.next = e
	} else {
		c.head = e
	}
	c.tail = e
}

func (c *Cache[T]) unlink(e *entry[T]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

func (c *Cache[T]) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}
