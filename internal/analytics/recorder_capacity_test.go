// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func TestRecorderRecordsBeyondFlushThresholds(t *testing.T) {
	for _, mode := range []string{"slots", "memberships"} {
		t.Run(mode, func(t *testing.T) {
			client := valkeytest.Spawn(t)
			names := make([]string, 4100)
			for i := range names {
				names[i] = "module" + strconv.Itoa(i)
			}
			r := analytics.New(client, zap.NewNop(), map[catalog.Kind][]string{catalog.KindModule: names}, analytics.WithClock(func() time.Time { return day }), analytics.WithoutTicker())
			t.Cleanup(r.Close)
			recordCapacityEntries(r, names, mode)
			r.Flush(t.Context())
			checkRecorderCapacity(t, client, mode)
		})
	}
}

func recordCapacityEntries(r *analytics.Recorder, names []string, mode string) {
	if mode == "memberships" {
		for id := uint64(1); id <= 22000; id++ {
			r.Distinct(catalog.ChannelsActive, id)
		}
		return
	}
	for _, name := range names {
		r.Count(catalog.ModuleEvents, name)
		r.Distinct(catalog.ModuleActive, 1, name)
	}
}

func checkRecorderCapacity(t *testing.T, client valkey.Client, mode string) {
	t.Helper()
	if mode == "memberships" {
		for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
			require.Equal(t, int64(22000), scard(t, client, catalog.DistinctKey(window, day, "channels.active")))
		}
		return
	}
	counts, err := client.Do(t.Context(), client.B().Hgetall().Key(catalog.CounterKey(day)).Build()).AsIntMap()
	require.NoError(t, err)
	require.Len(t, counts, 4100)
	for i := 0; i < 4100; i++ {
		name := "module" + strconv.Itoa(i)
		require.Equal(t, int64(1), counts[catalog.Field(catalog.ModuleEvents, name)])
		require.Equal(t, int64(1), scard(t, client, catalog.DistinctKey(catalog.Day, day, catalog.Field(catalog.ModuleActive, name))))
	}
}
