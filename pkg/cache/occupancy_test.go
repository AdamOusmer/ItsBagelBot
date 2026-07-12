package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogOccupancyEmitsPerCacheFields(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core)

	users := New[int](4096, time.Minute)
	defer users.Close()
	commands := New[int](8192, time.Minute)
	defer commands.Close()

	users.Set("a", 1)
	users.client.Wait() // drain the async write buffer so Len is accurate

	LogOccupancy(log, map[string]OccupancySource{
		"users":    users,
		"commands": commands,
	})

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, "cache occupancy", entries[0].Message)

	fields := entries[0].ContextMap()
	assert.Equal(t, int64(1), fields["users_entries"])
	assert.Equal(t, int64(4096), fields["users_capacity"])
	assert.Equal(t, int64(0), fields["commands_entries"])
	assert.Equal(t, int64(8192), fields["commands_capacity"])
}

func TestLogOccupancyNoCachesNoLine(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	LogOccupancy(zap.New(core), map[string]OccupancySource{})
	assert.Empty(t, logs.All(), "no caches means no log line")
}

func TestLogOccupancyNilLoggerIsSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		LogOccupancy(nil, map[string]OccupancySource{"x": New[int](1, time.Minute)})
	})
}

func TestStartOccupancyLoggerTicksThenStops(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core)

	c := New[int](16, time.Minute)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	StartOccupancyLogger(ctx, log, 10*time.Millisecond, map[string]OccupancySource{"c": c})

	assert.Eventually(t, func() bool {
		return logs.Len() >= 1
	}, time.Second, 5*time.Millisecond, "logger should emit at least one line")

	cancel()
	time.Sleep(30 * time.Millisecond)
	settled := logs.Len()
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, settled, logs.Len(), "no more lines after ctx is cancelled")
}

func TestStartOccupancyLoggerDisabledInterval(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	c := New[int](16, time.Minute)
	defer c.Close()

	StartOccupancyLogger(context.Background(), zap.New(core), 0, map[string]OccupancySource{"c": c})
	time.Sleep(30 * time.Millisecond)
	assert.Empty(t, logs.All(), "a non-positive interval starts no logger")
}
