// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/pkg/kvstate/kvtest"
	"github.com/stretchr/testify/require"
)

func TestJetStreamBatchFencesOwnersAndRetainsProgress(t *testing.T) {
	ctx := context.Background()
	kv := kvtest.New()
	first, second := NewJetStreamBatchStore(kv), NewJetStreamBatchStore(kv)
	one, two := BatchLease{"batch", "one"}, BatchLease{"batch", "two"}
	won, err := first.Acquire(ctx, one, batchStateTTL)
	require.NoError(t, err)
	require.True(t, won)
	won, err = second.Acquire(ctx, two, batchStateTTL)
	require.NoError(t, err)
	require.False(t, won)
	require.ErrorIs(t, second.SaveNext(ctx, two, 99, batchStateTTL), errBatchBusy)
	require.NoError(t, first.SaveNext(ctx, one, 2, batchStateTTL))
	require.NoError(t, first.Release(ctx, one))
	won, err = second.Acquire(ctx, two, batchStateTTL)
	require.NoError(t, err)
	require.True(t, won)
	require.ErrorIs(t, first.SaveNext(ctx, one, 99, batchStateTTL), errBatchBusy)
	require.ErrorIs(t, first.Release(ctx, one), errBatchBusy)
	next, err := second.Next(ctx, two.ID)
	require.NoError(t, err)
	require.Equal(t, 2, next)
}

func TestJetStreamBatchFailsClosedWithoutQuorum(t *testing.T) {
	kv := kvtest.New()
	kv.Err = errors.New("quorum unavailable")
	store := NewJetStreamBatchStore(kv)
	won, err := store.Acquire(context.Background(), BatchLease{"batch", "owner"}, batchStateTTL)
	require.Error(t, err)
	require.False(t, won)
}
