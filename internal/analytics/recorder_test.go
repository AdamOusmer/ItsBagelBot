// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const channel = uint64(804932984)

var day = time.Date(2030, 3, 14, 15, 4, 5, 0, time.UTC)

func newRecorder(t *testing.T, client valkey.Client, at *time.Time) *analytics.Recorder {
	t.Helper()
	r := analytics.New(client, zap.NewNop(), map[catalog.Kind][]string{
		catalog.KindModule:  {"quotes", "loyalty"},
		catalog.KindCommand: {"followage"},
	}, analytics.WithClock(func() time.Time { return *at }), analytics.WithoutTicker())
	t.Cleanup(r.Close)
	return r
}

func TestFlushWritesAggregatesOnly(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)

	r.Count(catalog.CommandUses, "followage")
	r.Count(catalog.CommandUses, "followage")
	r.Add(catalog.ModuleEvents, 5, "quotes")
	r.Distinct(catalog.ChannelsActive, channel)
	r.Distinct(catalog.ModuleActive, channel, "loyalty")
	r.Cohort(channel, day.AddDate(0, 0, -21))
	r.Flush(ctx)

	counts, err := client.Do(ctx, client.B().Hgetall().Key(catalog.CounterKey(day)).Build()).AsStrMap()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"command.uses|followage": "2", "module.events|quotes": "5"}, counts)

	for _, key := range []string{
		catalog.DistinctKey(catalog.Day, day, "channels.active"),
		catalog.DistinctKey(catalog.Week, day, "channels.active"),
		catalog.DistinctKey(catalog.Month, day, "channels.active"),
		catalog.DistinctKey(catalog.Day, day, "module.active|loyalty"),
		catalog.CohortKey(day.AddDate(0, 0, -21), day),
	} {
		require.Equal(t, int64(1), pfcount(t, client, key), key)
	}
	monthly, err := client.Do(ctx, client.B().Exists().Key(catalog.DistinctKey(catalog.Month, day, "module.active|loyalty")).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, monthly)
	weekIndex, err := client.Do(ctx, client.B().Smembers().Key(catalog.IndexKey(catalog.Week, day)).Build()).AsStrSlice()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		catalog.DistinctKey(catalog.Week, day, "channels.active"),
		catalog.DistinctKey(catalog.Week, day, "module.active|loyalty"),
		catalog.CohortKey(day.AddDate(0, 0, -21), day),
	}, weekIndex)

	requireNoIdentity(t, client)
	requireEveryKeyExpires(t, client)
}

func TestRecorderDropsValuesOutsideCatalog(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)

	r.Count(catalog.CommandUses, "!hug")
	r.Count(catalog.CommandUses, "hug")
	r.Count(catalog.ModuleEvents, "unregistered")
	r.Count(catalog.ModuleEvents)
	r.Count(catalog.ModuleErrors, "quotes", "stack trace text")
	r.Count(catalog.AccountsOnboarded)
	r.Count("made.up")
	r.Distinct(catalog.ChannelsActive, 0)
	r.Count(catalog.CommandUses, catalog.CustomCommand)
	r.Flush(ctx)

	counts, err := client.Do(ctx, client.B().Hgetall().Key(catalog.CounterKey(day)).Build()).AsStrMap()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"command.uses|custom": "1"}, counts)
}

func TestPodsShareSaltWithinPeriodAndRotateAcross(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	podA := newRecorder(t, client, &at)
	podB := newRecorder(t, client, &at)

	podA.Distinct(catalog.ChannelsActive, channel)
	podA.Flush(ctx)
	podB.Distinct(catalog.ChannelsActive, channel)
	podB.Distinct(catalog.ChannelsActive, channel+1)
	podB.Flush(ctx)
	require.Equal(t, int64(2), pfcount(t, client, catalog.DistinctKey(catalog.Day, day, "channels.active")))

	saltToday := get(t, client, catalog.SaltKey(catalog.Day, day))
	at = day.AddDate(0, 0, 1)
	podA.Distinct(catalog.ChannelsActive, channel)
	podA.Flush(ctx)
	require.NotEqual(t, saltToday, get(t, client, catalog.SaltKey(catalog.Day, at)))
	require.Equal(t, int64(1), pfcount(t, client, catalog.DistinctKey(catalog.Day, at, "channels.active")))

	saltTTL, err := client.Do(ctx, client.B().Expiretime().Key(catalog.SaltKey(catalog.Day, day)).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, catalog.SaltExpiry(catalog.Day, day).Unix(), saltTTL)
}

func TestNilRecorderIsInert(t *testing.T) {
	var r *analytics.Recorder
	r.Count(catalog.ChatMessages)
	r.Distinct(catalog.ChannelsActive, channel)
	r.Cohort(channel, day)
	r.Flush(context.Background())
	r.Close()
}

func pfcount(t *testing.T, client valkey.Client, key string) int64 {
	t.Helper()
	n, err := client.Do(context.Background(), client.B().Pfcount().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	return n
}

func get(t *testing.T, client valkey.Client, key string) string {
	t.Helper()
	v, err := client.Do(context.Background(), client.B().Get().Key(key).Build()).ToString()
	require.NoError(t, err)
	return v
}

func allKeys(t *testing.T, client valkey.Client) []string {
	t.Helper()
	keys, err := client.Do(context.Background(), client.B().Keys().Pattern("*").Build()).AsStrSlice()
	require.NoError(t, err)
	return keys
}

func requireNoIdentity(t *testing.T, client valkey.Client) {
	t.Helper()
	id := strconv.FormatUint(channel, 10)
	for _, key := range allKeys(t, client) {
		require.NotContains(t, key, id)
		dump, err := client.Do(context.Background(), client.B().Dump().Key(key).Build()).ToString()
		require.NoError(t, err)
		require.False(t, strings.Contains(dump, id), "key %s holds the raw id", key)
	}
}

func requireEveryKeyExpires(t *testing.T, client valkey.Client) {
	t.Helper()
	for _, key := range allKeys(t, client) {
		ttl, err := client.Do(context.Background(), client.B().Ttl().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		require.Positive(t, ttl, key)
	}
}

func TestDistinctDedupesWithinAPeriodWhileTheClockMoves(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)

	for id := uint64(1); id <= 5000; id++ {
		at = at.Add(time.Millisecond)
		r.Distinct(catalog.ChannelsActive, id)
		r.Distinct(catalog.ChannelsActive, id)
	}
	r.Flush(ctx)

	require.InDelta(t, 5000, pfcount(t, client, catalog.DistinctKey(catalog.Day, day, "channels.active")), 100)
}

func TestShiftMovesRunningTotalsByTheWriteDifference(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Spawn(t)
	at := day
	r := newRecorder(t, client, &at)

	before := analytics.Tally{}
	before.Add(catalog.ModulesRows, "quotes", "off")
	after := analytics.Tally{}
	after.Add(catalog.ModulesRows, "quotes", "on")
	after.Add(catalog.ModulesRows, "Streamer's Module", "on")
	r.Shift(analytics.Tally{}, before)
	r.Shift(before, after)
	r.Flush(ctx)

	levels, err := client.Do(ctx, client.B().Hgetall().Key(catalog.LevelKey).Build()).AsIntMap()
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"modules.rows|quotes|on": 1}, levels)
	ttl, err := client.Do(ctx, client.B().Ttl().Key(catalog.LevelKey).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, int64(-1), ttl)
}
