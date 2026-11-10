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
	"github.com/valkey-io/valkey-go"
)

func levels(t *testing.T, client valkey.Client) map[string]int64 {
	t.Helper()
	got, err := client.Do(context.Background(), client.B().Hgetall().Key(catalog.LevelKey).Build()).AsIntMap()
	require.NoError(t, err)
	return got
}

func TestReconcileRewritesOwnedTotalsOncePerClaimDay(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(catalog.LevelKey).FieldValue().
		FieldValue("modules.rows|quotes|on", "3").
		FieldValue("modules.rows|timers|off", "2").
		FieldValue("modules.breadth|6+", "4").
		FieldValue("integrations.connected|fetch_key", "5").
		FieldValue("custom.channels", "9").Build()).Error())

	runs := 0
	count := int64(40)
	owner := analytics.Owner{
		Service: "modules",
		Owned:   analytics.Owned{"modules.rows", "modules.breadth", "integrations.connected|govee"},
		Seed: func(context.Context) (analytics.Tally, error) {
			runs++
			return analytics.Tally{"modules.rows|quotes|on": count, "modules.breadth|4": 1}, nil
		},
	}
	r.Reconcile(ctx, owner)
	r.Reconcile(ctx, owner)
	first := levels(t, client)
	firstRuns := runs

	at = at.Add(24 * time.Hour)
	count = 7
	r.Reconcile(ctx, owner)

	type outcome struct {
		First     map[string]int64
		FirstRuns int
		Runs      int
		NextDay   int64
	}
	require.Equal(t, outcome{
		First: map[string]int64{
			"modules.rows|quotes|on":           40,
			"modules.breadth|4":                1,
			"integrations.connected|fetch_key": 5,
			"custom.channels":                  9,
		},
		FirstRuns: 1,
		Runs:      2,
		NextDay:   7,
	}, outcome{First: first, FirstRuns: firstRuns, Runs: runs, NextDay: levels(t, client)["modules.rows|quotes|on"]})
}

func ttl(t *testing.T, client valkey.Client, key string) int64 {
	t.Helper()
	n, err := client.Do(context.Background(), client.B().Ttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	return n
}

func TestReconcileClaimsTheDayAboutToStart(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := time.Date(2030, 3, 14, 23, 30, 0, 0, time.UTC)
	r := newRecorder(t, client, &at)

	r.Reconcile(ctx, analytics.Owner{Service: "users", Seed: func(context.Context) (analytics.Tally, error) {
		return analytics.Tally{}, nil
	}})

	next := ttl(t, client, catalog.ReconcileClaimKey("users", at.AddDate(0, 0, 1)))
	today := ttl(t, client, catalog.ReconcileClaimKey("users", at))
	require.Equal(t, []bool{true, true}, []bool{next > 0, today == -2})
}

func TestFailedReconcileReleasesItsClaim(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)

	r.Reconcile(ctx, analytics.Owner{Service: "commands", Owned: analytics.Owned{"custom.channels"},
		Seed: func(context.Context) (analytics.Tally, error) { return nil, errors.New("db down") }})

	exists, err := client.Do(ctx, client.B().Exists().Key(catalog.ReconcileClaimKey("commands", day)).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, exists)
}

func TestReconcileDailyStopsWhenContextEnds(t *testing.T) {
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{})
	done := make(chan struct{})
	go func() {
		r.ReconcileDaily(ctx, analytics.Owner{Service: "users", Seed: func(context.Context) (analytics.Tally, error) {
			close(ran)
			return analytics.Tally{}, nil
		}})
		close(done)
	}()

	<-ran
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReconcileDaily kept running after its context ended")
	}
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
