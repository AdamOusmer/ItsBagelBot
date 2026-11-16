// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	"github.com/stretchr/testify/require"
)

func TestLevelSourceCachesOwnedCatalogFieldsForSixtySeconds(t *testing.T) {
	now := time.Now()
	calls := 0
	source := analytics.NewLevelSource(analytics.Owner{Owned: analytics.Owned{"accounts.total", "accounts.onboarded"}, Seed: func(context.Context) (analytics.Tally, error) {
		calls++
		return analytics.Tally{"accounts.total|free": int64(calls), "accounts.total|unknown": 12, "accounts.onboarded": -4, "custom.channels": 7, "lifecycle|signup": 3}, nil
	}}, analytics.WithLevelClock(func() time.Time { return now }))
	for _, step := range []struct {
		age   time.Duration
		calls int
	}{{0, 1}, {59 * time.Second, 1}, {time.Second, 2}} {
		now = now.Add(step.age)
		got, err := source.Levels(t.Context())
		require.NoError(t, err)
		require.Equal(t, analytics.Tally{"accounts.total|free": int64(step.calls), "accounts.onboarded": 0}, got)
		got["accounts.total|free"] = 999
		require.Equal(t, step.calls, calls)
	}
}

func TestLevelSourceSharesConcurrentLoads(t *testing.T) {
	var calls atomic.Int64
	started, release := make(chan struct{}), make(chan struct{})
	source := analytics.NewLevelSource(analytics.Owner{Owned: analytics.Owned{"custom.channels"}, Seed: func(ctx context.Context) (analytics.Tally, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return analytics.Tally{"custom.channels": 3}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			got, err := source.Levels(t.Context())
			if err != nil || got["custom.channels"] != 3 {
				t.Errorf("load = %v, %v", got, err)
			}
		})
	}
	<-started
	close(release)
	group.Wait()
	require.EqualValues(t, 1, calls.Load())
}

func TestLevelSourceDoesNotCacheFailuresOrServeExpiredValues(t *testing.T) {
	now := time.Now()
	calls := 0
	dbError := errors.New("SQL unavailable")
	source := analytics.NewLevelSource(analytics.Owner{Owned: analytics.Owned{"custom.channels"}, Seed: func(context.Context) (analytics.Tally, error) {
		calls++
		if calls == 1 {
			return analytics.Tally{"custom.channels": 5}, nil
		}
		return nil, dbError
	}}, analytics.WithLevelClock(func() time.Time { return now }))
	_, err := source.Levels(t.Context())
	require.NoError(t, err)
	now = now.Add(time.Minute)
	for range 2 {
		values, err := source.Levels(t.Context())
		require.ErrorIs(t, err, dbError)
		require.Nil(t, values)
	}
	require.Equal(t, 3, calls)
}

func TestLevelSourceWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	source := analytics.NewLevelSource(analytics.Owner{Seed: func(ctx context.Context) (analytics.Tally, error) { return nil, ctx.Err() }})
	_, err := source.Levels(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
