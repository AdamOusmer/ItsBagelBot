// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestStartOccupancyLoggerReportsEveryCacheThenStops(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	users := cache.New[int](4096, time.Minute)
	defer users.Close()
	commands := cache.New[int](8192, time.Minute)
	defer commands.Close()
	users.Set("a", 1)
	require.Eventually(t, func() bool { return users.Len() == 1 }, time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cache.StartOccupancyLogger(ctx, zap.New(core), 10*time.Millisecond, map[string]cache.OccupancySource{
		"users":    users,
		"commands": commands,
	})

	require.Eventually(t, func() bool { return logs.Len() >= 1 }, time.Second, 5*time.Millisecond)
	entry := logs.All()[0]
	assert.Equal(t, "cache occupancy", entry.Message)
	assert.Equal(t, map[string]any{
		"users_entries": int64(1), "users_capacity": int64(4096),
		"commands_entries": int64(0), "commands_capacity": int64(8192),
	}, entry.ContextMap())

	cancel()
	time.Sleep(30 * time.Millisecond)
	settled := logs.Len()
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, settled, logs.Len(), "no more lines after ctx is cancelled")
}

func TestStartOccupancyLoggerStaysOffWhenNothingToReport(t *testing.T) {
	one := cache.New[int](16, time.Minute)
	defer one.Close()
	core, logs := observer.New(zap.InfoLevel)
	tests := []struct {
		name     string
		log      *zap.Logger
		interval time.Duration
		caches   map[string]cache.OccupancySource
	}{
		{name: "starts nothing for a non-positive interval", log: zap.New(core), caches: map[string]cache.OccupancySource{"c": one}},
		{name: "starts nothing without caches", log: zap.New(core), interval: 5 * time.Millisecond},
		{name: "starts nothing without a logger", interval: 5 * time.Millisecond, caches: map[string]cache.OccupancySource{"c": one}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				cache.StartOccupancyLogger(context.Background(), tc.log, tc.interval, tc.caches)
			})
		})
	}

	time.Sleep(30 * time.Millisecond)
	assert.Empty(t, logs.All())
}
