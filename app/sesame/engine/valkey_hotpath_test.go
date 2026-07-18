package engine

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

// These tests are opt-in because atomic script semantics need a real Valkey
// interpreter. They use the same VALKEY_TEST_ADDR convention as pkg/ratelimit.
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
		keySuffix: ":counter", script: bumpChannelScript,
		missingArgs: []string{"", "2", "60"},
		seedArgs:    []string{"41", "2", "60"}, seedValue: 43,
		warmArgs: []string{"", "3", "60"}, warmValue: 46,
	})
	f.requireConcurrentBumps(t, key, 46)
}

func (f hotPathFixture) requireConcurrentBumps(t *testing.T, key string, initial int) {
	const replicas = 16
	errs := make(chan error, replicas)
	var wg sync.WaitGroup
	wg.Add(replicas)
	for range replicas {
		go func() {
			defer wg.Done()
			errs <- bumpChannelScript.Exec(context.Background(), f.client,
				[]string{key}, []string{"", "1", "60"}).Error()
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
		keySuffix: ":entries", script: bumpEntryScript,
		missingArgs: []string{"viewer", "", "1", "60"},
		seedArgs:    []string{"viewer", "9", "1", "60"}, seedValue: 10,
		warmArgs: []string{"viewer", "", "4", "60"}, warmValue: 14,
	})
}

type bumpCase struct {
	keySuffix   string
	script      *valkey.Lua
	missingArgs []string
	seedArgs    []string
	seedValue   int64
	warmArgs    []string
	warmValue   int64
}

func (f hotPathFixture) testBump(t *testing.T, test bumpCase) string {
	key := f.prefix + test.keySuffix
	f.cleanupKeys(t, key)
	_, err := test.script.Exec(f.ctx, f.client, []string{key}, test.missingArgs).AsInt64()
	require.True(t, valkey.IsValkeyNil(err), "empty seed must decode as Valkey nil")
	seeded, err := test.script.Exec(f.ctx, f.client, []string{key}, test.seedArgs).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, test.seedValue, seeded)
	warm, err := test.script.Exec(f.ctx, f.client, []string{key}, test.warmArgs).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, test.warmValue, warm)
	requirePositiveTTL(t, f.ctx, f.client, key)
	return key
}

func (f hotPathFixture) testGreetClaim(t *testing.T) {
	key := f.prefix + ":greet"
	f.cleanupKeys(t, key)
	first, err := firstGreetScript.Exec(f.ctx, f.client, []string{key}, []string{"viewer", "60"}).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, first)
	second, err := firstGreetScript.Exec(f.ctx, f.client, []string{key}, []string{"viewer", "60"}).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 0, second)
	requirePositiveTTL(t, f.ctx, f.client, key)
}

func (f hotPathFixture) testFeedCounters(t *testing.T) {
	totalKey, todayKey := f.prefix+":feed-total", f.prefix+":feed-today"
	f.cleanupKeys(t, totalKey, todayKey)
	_, err := decodeFeedCounts(feedWarmScript.Exec(f.ctx, f.client, []string{totalKey, todayKey}, []string{"60"}))
	require.True(t, valkey.IsValkeyNil(err), "missing total must decode as Valkey nil")
	seeded, err := decodeFeedCounts(feedSeedScript.Exec(f.ctx, f.client, []string{totalKey, todayKey}, []string{"100", "60"}))
	require.NoError(t, err)
	require.Equal(t, FeedCounts{Today: 1, Total: 100}, seeded)
	warm, err := decodeFeedCounts(feedWarmScript.Exec(f.ctx, f.client, []string{totalKey, todayKey}, []string{"60"}))
	require.NoError(t, err)
	require.Equal(t, FeedCounts{Today: 2, Total: 101}, warm)
	stale, err := decodeFeedCounts(feedSeedScript.Exec(f.ctx, f.client, []string{totalKey, todayKey}, []string{"99", "60"}))
	require.NoError(t, err)
	require.Equal(t, FeedCounts{Today: 3, Total: 101}, stale)
	requirePositiveTTL(t, f.ctx, f.client, todayKey)
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
	b.ReportAllocs()
	for b.Loop() {
		if err := bumpChannelScript.Exec(f.ctx, f.client, []string{f.counter}, []string{"", "1", "60"}).Error(); err != nil {
			b.Fatal(err)
		}
	}
}

func (f hotPathBenchmark) benchmarkFeed(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := decodeFeedCounts(feedWarmScript.Exec(f.ctx, f.client,
			[]string{f.total, f.today}, []string{"60"})); err != nil {
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
		tb.Skip("VALKEY_TEST_ADDR is not set")
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
