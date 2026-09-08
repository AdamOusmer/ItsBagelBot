// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gateWaitBudget is how long the uncontended acquire is given. Enormous
// relative to a channel send, so this asserts "did not block" without being a
// timing test that a loaded CI machine can lose.
const gateWaitBudget = 250 * time.Millisecond

func TestAcquireFastPathDoesNotBlock(t *testing.T) {
	slots := newGate(1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		release, err := acquireFrom(context.Background(), slots)
		require.NoError(t, err)
		release()
	}()

	select {
	case <-done:
	case <-time.After(gateWaitBudget):
		t.Fatal("acquire blocked on an empty gate")
	}
}

func TestAcquireSlowPathHonoursDeadline(t *testing.T) {
	slots := newGate(1)

	release, err := acquireFrom(context.Background(), slots)
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	blocked, err := acquireFrom(ctx, slots)
	require.Nil(t, blocked)
	require.ErrorContains(t, err, "db concurrency gate")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// A released slot has to be returned to the same gate, or the second acquire
// below waits forever on a semaphore that reports itself full.
func TestReleaseReturnsTheSlot(t *testing.T) {
	slots := newGate(1)

	release, err := acquireFrom(context.Background(), slots)
	require.NoError(t, err)
	release()

	ctx, cancel := context.WithTimeout(context.Background(), gateWaitBudget)
	defer cancel()

	again, err := acquireFrom(ctx, slots)
	require.NoError(t, err)
	again()
}

func TestNewGateFallsBackOnNonPositiveSize(t *testing.T) {
	require.Equal(t, defaultMaxConns, cap(newGate(0)))
	require.Equal(t, defaultMaxConns, cap(newGate(-1)))
	require.Equal(t, 3, cap(newGate(3)))
}
