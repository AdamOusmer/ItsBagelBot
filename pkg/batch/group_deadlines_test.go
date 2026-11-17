// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/batch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupQueueExpiryNeverFlushesExpiredItems(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	g := batch.NewGroup(batch.GroupConfig[int]{
		MaxRows: 1, MaxBytes: 10, Size: func(int) int { return 1 },
		QueueTimeout: 20 * time.Millisecond,
	}, func(context.Context, []int) error {
		calls.Add(1)
		close(entered)
		<-release
		return nil
	})
	first := make(chan error, 1)
	go func() { first <- g.Submit(t.Context(), 1) }()
	<-entered
	require.ErrorIs(t, g.Submit(t.Context(), 2), context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, g.Close(t.Context()))
	assert.EqualValues(t, 1, calls.Load())
}

func TestGroupPublicationHasIndependentBudget(t *testing.T) {
	var publications atomic.Int32
	g := batch.NewGroup(batch.GroupConfig[int]{
		MaxRows: 1, MaxBytes: 10, Size: func(int) int { return 1 },
		Timeout: 10 * time.Millisecond, AfterCommitTimeout: time.Second,
		AfterCommit: func(ctx context.Context, items []int) {
			assert.NoError(t, ctx.Err())
			deadline, ok := ctx.Deadline()
			assert.True(t, ok)
			assert.Greater(t, time.Until(deadline), 500*time.Millisecond)
			publications.Add(int32(len(items)))
		},
	}, func(ctx context.Context, _ []int) error {
		<-ctx.Done()
		return nil
	})
	require.NoError(t, g.Submit(t.Context(), 1))
	require.NoError(t, g.Close(t.Context()))
	assert.EqualValues(t, 1, publications.Load())
}

func TestGroupQueueBudgetIncludesWindow(t *testing.T) {
	var calls atomic.Int32
	g := batch.NewGroup(batch.GroupConfig[int]{
		Window: time.Hour, MaxRows: 256, MaxBytes: 1024, Size: func(int) int { return 1 },
		QueueTimeout: 10 * time.Millisecond,
	}, func(context.Context, []int) error {
		calls.Add(1)
		return nil
	})
	require.ErrorIs(t, g.Submit(t.Context(), 1), context.DeadlineExceeded)
	require.NoError(t, g.Close(t.Context()))
	assert.Zero(t, calls.Load())
}
