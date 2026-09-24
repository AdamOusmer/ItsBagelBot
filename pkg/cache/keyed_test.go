// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func uintKey(id uint64) string { return strconv.FormatUint(id, 10) }

func TestKeyedHitSavesStringAlloc(t *testing.T) {
	ctx := context.Background()
	const id uint64 = 123456789

	keyed := NewKeyed[uint64, bool](DefaultCapacity, time.Minute, uintKey)
	defer keyed.Close()
	loader := func(context.Context) (bool, error) { return true, nil }
	if _, err := keyed.GetOrLoad(ctx, id, loader); err != nil {
		t.Fatal(err)
	}

	stringKeyed := New[bool](DefaultCapacity, time.Minute)
	defer stringKeyed.Close()
	if _, err := stringKeyed.GetOrLoad(ctx, uintKey(id), loader); err != nil {
		t.Fatal(err)
	}

	uintAllocs := testing.AllocsPerRun(1000, func() {
		_, _ = keyed.GetOrLoad(ctx, id, loader)
	})
	stringAllocs := testing.AllocsPerRun(1000, func() {
		_, _ = stringKeyed.GetOrLoad(ctx, uintKey(id), loader)
	})

	if uintAllocs > 1 {
		t.Fatalf("uint64 hit allocated %.1f allocs/op, want <=1 (theine read-tracking floor)", uintAllocs)
	}
	if uintAllocs >= stringAllocs {
		t.Fatalf("uint64 hit (%.1f) should allocate strictly less than the string-keyed hit (%.1f)", uintAllocs, stringAllocs)
	}
}

func TestKeyedSingleflightCollapsesMisses(t *testing.T) {
	c := NewKeyed[uint64, bool](DefaultCapacity, time.Minute, uintKey)
	defer c.Close()

	var calls atomic.Int64
	release := make(chan struct{})
	loader := func(context.Context) (bool, error) {
		calls.Add(1)
		<-release
		return true, nil
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = c.GetOrLoad(context.Background(), 7, loader)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("loader ran %d times, want 1 (singleflight should collapse)", got)
	}
}

func TestKeyedInvalidateReloads(t *testing.T) {
	c := NewKeyed[uint64, bool](DefaultCapacity, time.Minute, uintKey)
	defer c.Close()

	ctx := context.Background()
	var val atomic.Bool
	val.Store(true)
	loader := func(context.Context) (bool, error) { return val.Load(), nil }

	if got, _ := c.GetOrLoad(ctx, 1, loader); !got {
		t.Fatal("first load should be true")
	}

	val.Store(false)
	if got, _ := c.GetOrLoad(ctx, 1, loader); !got {
		t.Fatal("cached value should still be true before invalidation")
	}

	c.Invalidate(1)
	if got, _ := c.GetOrLoad(ctx, 1, loader); got {
		t.Fatal("after invalidation the loader should re-run and return false")
	}
}
