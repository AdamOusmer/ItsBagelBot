// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"
)

const lockKey = "test:lock"

func mustWin(t *testing.T, what string, won bool, err error) {
	t.Helper()
	if err != nil || !won {
		t.Fatalf("%s: won=%v err=%v, want true/nil", what, won, err)
	}
}

func mustLose(t *testing.T, what string, won bool, err error) {
	t.Helper()
	if won || err != nil {
		t.Fatalf("%s: won=%v err=%v, want false/nil", what, won, err)
	}
}

func mustFail(t *testing.T, what string, won bool, err error) {
	t.Helper()
	if won || err == nil {
		t.Fatalf("%s: won=%v err=%v, want false/non-nil", what, won, err)
	}
}

func mustHold(t *testing.T, f *lockFake, key, owner string) {
	t.Helper()
	got, ok := f.value(key)
	if !ok || got != owner {
		t.Fatalf("lock %s = %q present=%v, want held by %q", key, got, ok, owner)
	}
}

func TestOwnerLockAcquireIsExclusive(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	first := NewOwnerLock(f.client, lockKey, "pod-a")
	second := NewOwnerLock(f.client, lockKey, "pod-b")

	won, err := first.Acquire(ctx, time.Minute)
	mustWin(t, "first acquire", won, err)
	won, err = second.Acquire(ctx, time.Minute)
	mustLose(t, "contended acquire", won, err)
	mustHold(t, f, lockKey, "pod-a")
}

func TestOwnerLockReleaseIgnoresForeignOwner(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	won, err := holder.Acquire(ctx, time.Minute)
	mustWin(t, "acquire", won, err)

	if err := other.Release(ctx); err != nil {
		t.Fatalf("foreign release: %v", err)
	}
	mustHold(t, f, lockKey, "pod-a")

	if err := holder.Release(ctx); err != nil {
		t.Fatalf("owner release: %v", err)
	}
	won, err = other.Acquire(ctx, time.Minute)
	mustWin(t, "acquire after release", won, err)
}

func TestOwnerLockReleaseOfAbsentKeyIsNoError(t *testing.T) {
	f := newLockFake(t)
	if err := NewOwnerLock(f.client, lockKey, "pod-a").Release(context.Background()); err != nil {
		t.Fatalf("release of an expired lock: %v", err)
	}
}

func TestOwnerLockAcquireReportsBackendFailure(t *testing.T) {
	f := newLockFake(t)
	f.breakSET()
	won, err := NewOwnerLock(f.client, lockKey, "pod-a").Acquire(context.Background(), time.Minute)
	mustFail(t, "acquire against a broken backend", won, err)
}

func TestOwnerLockAcquireExpiresWithMillisecondPrecision(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	won, err := holder.Acquire(ctx, 1500*time.Millisecond)
	mustWin(t, "acquire", won, err)

	f.advance(time.Second)
	won, err = other.Acquire(ctx, time.Minute)
	mustLose(t, "acquire at 1s of a 1500ms TTL", won, err)

	f.advance(600 * time.Millisecond)
	won, err = other.Acquire(ctx, time.Minute)
	mustWin(t, "acquire after expiry", won, err)
}

func TestClaimOnceHasOneWinnerPerTTL(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	const key = "test:claim"

	won, err := ClaimOnce(ctx, f.client, key, 30*time.Second)
	mustWin(t, "first claim", won, err)
	won, err = ClaimOnce(ctx, f.client, key, 30*time.Second)
	mustLose(t, "second claim", won, err)

	f.advance(30 * time.Second)
	won, err = ClaimOnce(ctx, f.client, key, 30*time.Second)
	mustWin(t, "claim after expiry", won, err)
}

func TestClaimOnceReportsBackendFailure(t *testing.T) {
	f := newLockFake(t)
	f.breakSET()
	won, err := ClaimOnce(context.Background(), f.client, "test:claim", 30*time.Second)
	mustFail(t, "claim against a broken backend", won, err)
}
