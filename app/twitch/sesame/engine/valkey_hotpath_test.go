// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestValkeyHotPathScriptsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	fixture := hotPathFixture{
		ctx:    ctx,
		client: newHotPathTestClient(t),
		prefix: "test:sesame:hot:" + strconv.FormatInt(time.Now().UnixNano(), 10),
	}
	t.Run("loyalty channel seed and warm bump", fixture.testChannelBump)
	t.Run("loyalty entry seed and warm bump", fixture.testEntryBump)
	t.Run("greet claim and expiry", fixture.testGreetClaim)
	t.Run("feed counters share one atomic call", fixture.testFeedCounters)
}

func BenchmarkValkeyHotPathScriptsIntegration(b *testing.B) {
	fixture := newHotPathBenchmark(b)
	b.Run("loyalty_warm_one_rtt", fixture.benchmarkLoyalty)
	b.Run("feed_warm_one_rtt", fixture.benchmarkFeed)
}

type hotPathFixture struct {
	ctx    context.Context
	client valkey.Client
	prefix string
}

func (f hotPathFixture) testChannelBump(t *testing.T) {
	key := f.testBump(t, bumpCase{
		keySuffix: ":counter", counter: pkg_valkey.SeededCounter{TTL: 60 * time.Second},
		seed: 41, seedDelta: 2, seedValue: 43,
		warmDelta: 3, warmValue: 46,
	})
	f.requireConcurrentBumps(t, key, 46)
}

func (f hotPathFixture) requireConcurrentBumps(t *testing.T, key string, initial int) {
	const replicas = 16
	counter := pkg_valkey.SeededCounter{Key: key, TTL: 60 * time.Second}
	errs := make(chan error, replicas)
	var wg sync.WaitGroup
	wg.Add(replicas)
	for range replicas {
		go func() {
			defer wg.Done()
			_, err := counter.Bump(context.Background(), f.client, 1, mustNotSeed(t))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	final, err := f.client.Do(f.ctx, f.client.B().Get().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, initial+replicas, final)
}

func (f hotPathFixture) testEntryBump(t *testing.T) {
	f.testBump(t, bumpCase{
		keySuffix: ":entries", counter: pkg_valkey.SeededCounter{Field: "viewer", TTL: 60 * time.Second},
		seed: 9, seedDelta: 1, seedValue: 10,
		warmDelta: 4, warmValue: 14,
	})
}

type bumpCase struct {
	keySuffix string
	counter   pkg_valkey.SeededCounter
	seed      int64
	seedDelta int64
	seedValue int64
	warmDelta int64
	warmValue int64
}

func (f hotPathFixture) testBump(t *testing.T, test bumpCase) string {
	key := f.prefix + test.keySuffix
	f.cleanupKeys(t, key)
	test.counter.Key = key
	seeded, err := test.counter.Bump(f.ctx, f.client, test.seedDelta, func() int64 { return test.seed })
	require.NoError(t, err)
	require.EqualValues(t, test.seedValue, seeded)
	warm, err := test.counter.Bump(f.ctx, f.client, test.warmDelta, mustNotSeed(t))
	require.NoError(t, err)
	require.EqualValues(t, test.warmValue, warm)
	requirePositiveTTL(t, f.ctx, f.client, key)
	return key
}

func mustNotSeed(t require.TestingT) func() int64 {
	return func() int64 {
		require.Fail(t, "seed must not be called once a counter is warm")
		return 0
	}
}

func (f hotPathFixture) testGreetClaim(t *testing.T) {
	key := f.prefix + ":greet"
	f.cleanupKeys(t, key)
	set := pkg_valkey.FreshMemberSet{Key: key}
	first, err := set.Add(f.ctx, f.client, "viewer", 60*time.Second)
	require.NoError(t, err)
	require.True(t, first)
	second, err := set.Add(f.ctx, f.client, "viewer", 60*time.Second)
	require.NoError(t, err)
	require.False(t, second)
	requirePositiveTTL(t, f.ctx, f.client, key)
}

func (f hotPathFixture) testFeedCounters(t *testing.T) {
	todayKey, receipt := f.prefix+":feed-today", f.prefix+":feed-receipt"
	f.cleanupKeys(t, todayKey, receipt, receipt+"2")
	counter := pkg_valkey.ReceiptCounter{Counter: todayKey, CounterTTL: 60 * time.Second}
	const maxCounter = int64(9223372036854775807)

	firstToday, firstTotal, err := counter.Bump(f.ctx, f.client, receipt, 120*time.Second, maxCounter)
	require.NoError(t, err)
	require.Equal(t, FeedCounts{Today: 1, Total: maxCounter}, FeedCounts{Today: firstToday, Total: firstTotal})

	replayToday, replayTotal, err := counter.Bump(f.ctx, f.client, receipt, 120*time.Second, maxCounter)
	require.NoError(t, err)
	require.Equal(t, firstToday, replayToday)
	require.Equal(t, firstTotal, replayTotal)

	require.NoError(t, f.client.Do(f.ctx, f.client.B().Set().Key(todayKey).Value("9223372036854775806").Build()).Error())
	lastToday, _, err := counter.Bump(f.ctx, f.client, receipt+"2", 120*time.Second, maxCounter)
	require.NoError(t, err)
	require.Equal(t, maxCounter, lastToday)

	_, _, err = counter.Bump(f.ctx, f.client, receipt+"3", 120*time.Second, maxCounter)
	require.Error(t, err)
}

func (f hotPathFixture) cleanupKeys(tb testingTB, keys ...string) {
	tb.Cleanup(func() {
		f.client.Do(context.Background(), f.client.B().Del().Key(keys...).Build())
	})
}

type hotPathBenchmark struct {
	ctx     context.Context
	client  valkey.Client
	counter string
	total   string
	today   string
}

func newHotPathBenchmark(b *testing.B) hotPathBenchmark {
	client := newHotPathTestClient(b)
	prefix := "bench:sesame:hot:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	fixture := hotPathBenchmark{
		ctx: context.Background(), client: client,
		counter: prefix + ":counter", total: prefix + ":total", today: prefix + ":today",
	}
	b.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(fixture.counter, fixture.total, fixture.today).Build())
	})
	require.NoError(b, client.Do(fixture.ctx, client.B().Set().Key(fixture.counter).Value("0").Build()).Error())
	require.NoError(b, client.Do(fixture.ctx, client.B().Set().Key(fixture.total).Value("1").Build()).Error())
	return fixture
}

func (f hotPathBenchmark) benchmarkLoyalty(b *testing.B) {
	counter := pkg_valkey.SeededCounter{Key: f.counter, TTL: 60 * time.Second}
	noSeed := func() int64 { b.Fatal("seed must not be called once a counter is warm"); return 0 }
	b.ReportAllocs()
	for b.Loop() {
		if _, err := counter.Bump(f.ctx, f.client, 1, noSeed); err != nil {
			b.Fatal(err)
		}
	}
}

func (f hotPathBenchmark) benchmarkFeed(b *testing.B) {
	counter := pkg_valkey.ReceiptCounter{Counter: f.today, CounterTTL: 60 * time.Second}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := counter.Bump(f.ctx, f.client, f.total+":receipt", 120*time.Second, 1); err != nil {
			b.Fatal(err)
		}
	}
}

type testingTB interface {
	Helper()
	Skip(args ...any)
	Fatal(args ...any)
	Cleanup(func())
}

func newHotPathTestClient(tb testingTB) valkey.Client {
	tb.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		address = localValkeyAddr(tb)
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{address},
		Password:    os.Getenv("VALKEY_TEST_PASSWORD"),
	})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(client.Close)
	return client
}

func requirePositiveTTL(t *testing.T, ctx context.Context, client valkey.Client, key string) {
	t.Helper()
	ttl, err := client.Do(ctx, client.B().Ttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	require.Positive(t, ttl)
}
