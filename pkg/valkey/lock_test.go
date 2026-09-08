// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"
)

const lockKey = "test:lock"

// The three outcomes a claim can have are asserted through named helpers
// rather than inline: each test then reads as the SEQUENCE it is about (take,
// contend, release, retake) instead of a wall of two-clause conditions, and
// the "a lost race is not an error" rule is stated once where it can be
// explained once.

// mustWin fails unless the caller took the key.
func mustWin(t *testing.T, what string, won bool, err error) {
	t.Helper()
	if err != nil || !won {
		t.Fatalf("%s: won=%v err=%v, want true/nil", what, won, err)
	}
}

// mustLose fails unless the caller was cleanly told someone else holds the
// key. valkey-go surfaces the declined NX as a Nil error, so a contended
// acquire must still report a NIL error: passing that through would turn every
// normal contention into a logged outage.
func mustLose(t *testing.T, what string, won bool, err error) {
	t.Helper()
	if won || err != nil {
		t.Fatalf("%s: won=%v err=%v, want false/nil", what, won, err)
	}
}

// mustFail fails unless a backend that cannot answer was reported as such. A
// failure must not look like a lost race: callers choose different behaviour
// for the two (back off vs proceed uncoordinated), so collapsing them would
// silently pick one.
func mustFail(t *testing.T, what string, won bool, err error) {
	t.Helper()
	if won || err == nil {
		t.Fatalf("%s: won=%v err=%v, want false/non-nil", what, won, err)
	}
}

// mustHold fails unless key is present and held under owner.
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

	// A late holder releasing a lock someone else now owns is the failure
	// releaseIfOwner exists to prevent, so a foreign release must be a no-op
	// AND must not report an error (a late arrival is not a fault).
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

// Acquire arms in milliseconds (PX), not seconds: a caller with a 1500ms lease
// must still hold it at 1s. An EX-based implementation would truncate to 1s
// and free the lock a third of a second early.
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

	// Nobody releases a claim; it lapses on its own and the next tick may win.
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
