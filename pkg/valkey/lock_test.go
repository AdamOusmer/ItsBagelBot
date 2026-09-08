// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"testing"
	"time"
)

const lockKey = "test:lock"

func TestOwnerLockAcquireIsExclusive(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	first := NewOwnerLock(f.client, lockKey, "pod-a")
	second := NewOwnerLock(f.client, lockKey, "pod-b")

	won, err := first.Acquire(ctx, time.Minute)
	if err != nil || !won {
		t.Fatalf("first acquire: won=%v err=%v", won, err)
	}
	// The second replica must learn it lost WITHOUT an error: valkey-go
	// surfaces the declined NX as a Nil error, and reporting that as a failure
	// would turn every normal contention into a logged outage.
	won, err = second.Acquire(ctx, time.Minute)
	if won || err != nil {
		t.Fatalf("contended acquire: won=%v err=%v, want false/nil", won, err)
	}
	if got, _ := f.value(lockKey); got != "pod-a" {
		t.Fatalf("lock value = %q, want the first owner's token", got)
	}
}

func TestOwnerLockReleaseIgnoresForeignOwner(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	if won, err := holder.Acquire(ctx, time.Minute); err != nil || !won {
		t.Fatalf("acquire: won=%v err=%v", won, err)
	}
	// A late holder releasing a lock someone else now owns is the failure
	// releaseIfOwner exists to prevent, so a foreign release must be a no-op
	// AND must not report an error (it is a normal late-arrival, not a fault).
	if err := other.Release(ctx); err != nil {
		t.Fatalf("foreign release: %v", err)
	}
	if got, ok := f.value(lockKey); !ok || got != "pod-a" {
		t.Fatalf("after foreign release value=%q present=%v, want the lock intact", got, ok)
	}
	if err := holder.Release(ctx); err != nil {
		t.Fatalf("owner release: %v", err)
	}
	if won, err := other.Acquire(ctx, time.Minute); err != nil || !won {
		t.Fatalf("acquire after release: won=%v err=%v", won, err)
	}
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
	// A backend that cannot answer must NOT look like a lost race: callers
	// choose different behaviour for the two (back off vs proceed
	// uncoordinated), so collapsing them would silently pick one.
	won, err := NewOwnerLock(f.client, lockKey, "pod-a").Acquire(context.Background(), time.Minute)
	if won || err == nil {
		t.Fatalf("acquire against a broken backend: won=%v err=%v, want false/non-nil", won, err)
	}
}

// Acquire arms in milliseconds (PX), not seconds: a caller with a 1500ms lease
// must still hold it at 1s. An EX-based implementation would truncate to 1s
// and free the lock a third of a second early.
func TestOwnerLockAcquireExpiresWithMillisecondPrecision(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	holder := NewOwnerLock(f.client, lockKey, "pod-a")
	other := NewOwnerLock(f.client, lockKey, "pod-b")

	if won, err := holder.Acquire(ctx, 1500*time.Millisecond); err != nil || !won {
		t.Fatalf("acquire: won=%v err=%v", won, err)
	}
	f.advance(time.Second)
	if won, _ := other.Acquire(ctx, time.Minute); won {
		t.Fatal("lock freed at 1s; a 1500ms TTL was truncated to seconds")
	}
	f.advance(600 * time.Millisecond)
	if won, err := other.Acquire(ctx, time.Minute); err != nil || !won {
		t.Fatalf("acquire after expiry: won=%v err=%v", won, err)
	}
}

func TestClaimOnceHasOneWinnerPerTTL(t *testing.T) {
	f := newLockFake(t)
	ctx := context.Background()
	const key = "test:claim"

	won, err := ClaimOnce(ctx, f.client, key, 30*time.Second)
	if err != nil || !won {
		t.Fatalf("first claim: won=%v err=%v", won, err)
	}
	// Losing is the common case (every other replica handling the same
	// expiry), so it must stay off the error path.
	won, err = ClaimOnce(ctx, f.client, key, 30*time.Second)
	if won || err != nil {
		t.Fatalf("second claim: won=%v err=%v, want false/nil", won, err)
	}
	// Nobody releases a claim; it lapses on its own and the next tick may win.
	f.advance(30 * time.Second)
	if won, err := ClaimOnce(ctx, f.client, key, 30*time.Second); err != nil || !won {
		t.Fatalf("claim after expiry: won=%v err=%v", won, err)
	}
}

func TestClaimOnceReportsBackendFailure(t *testing.T) {
	f := newLockFake(t)
	f.breakSET()
	won, err := ClaimOnce(context.Background(), f.client, "test:claim", 30*time.Second)
	if won || err == nil {
		t.Fatalf("claim against a broken backend: won=%v err=%v, want false/non-nil", won, err)
	}
}
