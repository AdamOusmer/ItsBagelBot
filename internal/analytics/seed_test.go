// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestSeedOnceOverwritesOwnedTotalsExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(catalog.LevelKey).FieldValue().
		FieldValue("modules.rows|quotes|on", "3").
		FieldValue("modules.rows|timers|off", "2").
		FieldValue("integrations.connected|fetch_key", "5").
		FieldValue("custom.channels", "9").Build()).Error())

	owned := []string{"modules.rows", "modules.breadth", "integrations.connected|govee"}
	runs := 0
	seed := func(context.Context) (analytics.Tally, error) {
		runs++
		tally := analytics.Tally{}
		for range 40 {
			tally.Add(catalog.ModulesRows, "quotes", "on")
		}
		return tally, nil
	}
	r.SeedOnce(ctx, "modules", owned, seed)
	r.SeedOnce(ctx, "modules", owned, seed)

	levels, err := client.Do(ctx, client.B().Hgetall().Key(catalog.LevelKey).Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"modules.rows|quotes|on":           40,
		"modules.rows|timers|off":          0,
		"integrations.connected|fetch_key": 5,
		"custom.channels":                  9,
	}, levels)
	require.Equal(t, 1, runs)
	marker, err := client.Do(ctx, client.B().Get().Key(catalog.SeedMarkerKey("modules")).Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "done", marker)
	ttl, err := client.Do(ctx, client.B().Ttl().Key(catalog.SeedMarkerKey("modules")).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(-1), ttl)
}

func TestFailedSeedReleasesItsClaim(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)

	r.SeedOnce(ctx, "commands", []string{"custom.channels"}, func(context.Context) (analytics.Tally, error) {
		return nil, errors.New("db down")
	})

	exists, err := client.Do(ctx, client.B().Exists().Key(catalog.SeedMarkerKey("commands")).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, exists)
}

func TestPagesWalksEveryRowInBoundedPages(t *testing.T) {
	rows := make([]int, 2500)
	for i := range rows {
		rows[i] = i + 1
	}
	var seen, calls int
	fetch := func(_ context.Context, after *int, limit int) ([]int, error) {
		calls++
		start := 0
		if after != nil {
			start = *after
		}
		return rows[start:min(start+limit, len(rows))], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, analytics.Pages(ctx, fetch, func(n int) int { return n }, func(int) { seen++ }))
	require.Equal(t, 2500, seen)
	require.Equal(t, 3, calls)
}
