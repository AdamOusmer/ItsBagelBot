// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import (
	"testing"
	"time"
)

func TestKeyedGetRespectsExpiryAndInvalidation(t *testing.T) {
	c := NewKeyed[uint64, bool](DefaultCapacity, time.Minute, uintKey)
	defer c.Close()
	if _, ok := c.Get(1); ok {
		t.Fatal("missing entry must not be reported as cached")
	}
	c.SetFor(1, false, time.Minute)
	if value, ok := c.Get(1); !ok || value {
		t.Fatal("cached zero value must be distinguishable from a miss")
	}
	c.Invalidate(1)
	if _, ok := c.Get(1); ok {
		t.Fatal("invalidated entry must be absent")
	}
	c.SetFor(2, true, time.Nanosecond)
	time.Sleep(time.Millisecond)
	if _, ok := c.Get(2); ok {
		t.Fatal("expired entry must be absent")
	}
}
