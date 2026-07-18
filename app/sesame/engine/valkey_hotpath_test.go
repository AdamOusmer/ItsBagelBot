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
	client := newHotPathTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	prefix := "test:sesame:hot:" + strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("loyalty channel seed and warm bump", func(t *testing.T) {
		key := prefix + ":counter"
		t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(key).Build()) })

		_, err := bumpChannelScript.Exec(ctx, client, []string{key}, []string{"", "2", "60"}).AsInt64()
		require.True(t, valkey.IsValkeyNil(err), "empty seed must decode as Valkey nil")

		seeded, err := bumpChannelScript.Exec(ctx, client, []string{key}, []string{"41", "2", "60"}).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 43, seeded)

		warm, err := bumpChannelScript.Exec(ctx, client, []string{key}, []string{"", "3", "60"}).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 46, warm)

		// The script is the only write for each caller, so concurrent replicas
		// cannot lose or double-apply an accepted delta.
		const replicas = 16
		errs := make(chan error, replicas)
		var wg sync.WaitGroup
		wg.Add(replicas)
		for range replicas {
			go func() {
				defer wg.Done()
				errs <- bumpChannelScript.Exec(context.Background(), client,
					[]string{key}, []string{"", "1", "60"}).Error()
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		final, err := client.Do(ctx, client.B().Get().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 46+replicas, final)
		requirePositiveTTL(t, ctx, client, key)
	})

	t.Run("loyalty entry seed and warm bump", func(t *testing.T) {
		key := prefix + ":entries"
		t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(key).Build()) })

		_, err := bumpEntryScript.Exec(ctx, client, []string{key}, []string{"viewer", "", "1", "60"}).AsInt64()
		require.True(t, valkey.IsValkeyNil(err), "empty seed must decode as Valkey nil")

		seeded, err := bumpEntryScript.Exec(ctx, client, []string{key}, []string{"viewer", "9", "1", "60"}).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 10, seeded)

		warm, err := bumpEntryScript.Exec(ctx, client, []string{key}, []string{"viewer", "", "4", "60"}).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 14, warm)
		requirePositiveTTL(t, ctx, client, key)
	})

	t.Run("greet claim and expiry", func(t *testing.T) {
		key := prefix + ":greet"
		t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(key).Build()) })

		first, err := firstGreetScript.Exec(ctx, client, []string{key}, []string{"viewer", "60"}).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 1, first)
		second, err := firstGreetScript.Exec(ctx, client, []string{key}, []string{"viewer", "60"}).AsInt64()
		require.NoError(t, err)
		require.EqualValues(t, 0, second)
		requirePositiveTTL(t, ctx, client, key)
	})

	t.Run("feed counters share one atomic call", func(t *testing.T) {
		totalKey, todayKey := prefix+":feed-total", prefix+":feed-today"
		t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(totalKey, todayKey).Build()) })

		_, err := decodeFeedCounts(feedWarmScript.Exec(ctx, client,
			[]string{totalKey, todayKey}, []string{"60"}))
		require.True(t, valkey.IsValkeyNil(err), "missing total must decode as Valkey nil")

		// The DB seed already includes this feeding, so seeding must preserve
		// 100 rather than incrementing it to 101. Only today's live window is
		// incremented by the cold completion script.
		seeded, err := decodeFeedCounts(feedSeedScript.Exec(ctx, client,
			[]string{totalKey, todayKey}, []string{"100", "60"}))
		require.NoError(t, err)
		require.Equal(t, FeedCounts{Today: 1, Total: 100}, seeded)

		warm, err := decodeFeedCounts(feedWarmScript.Exec(ctx, client,
			[]string{totalKey, todayKey}, []string{"60"}))
		require.NoError(t, err)
		require.Equal(t, FeedCounts{Today: 2, Total: 101}, warm)

		// A late cold seeder can only advance the view, never overwrite a
		// newer live total with an older database reply.
		stale, err := decodeFeedCounts(feedSeedScript.Exec(ctx, client,
			[]string{totalKey, todayKey}, []string{"99", "60"}))
		require.NoError(t, err)
		require.Equal(t, FeedCounts{Today: 3, Total: 101}, stale)
		requirePositiveTTL(t, ctx, client, todayKey)
	})
}

func BenchmarkValkeyHotPathScriptsIntegration(b *testing.B) {
	client := newHotPathTestClient(b)
	ctx := context.Background()
	prefix := "bench:sesame:hot:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	b.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(prefix+":counter", prefix+":total", prefix+":today").Build())
	})

	require.NoError(b, client.Do(ctx, client.B().Set().Key(prefix+":counter").Value("0").Build()).Error())
	require.NoError(b, client.Do(ctx, client.B().Set().Key(prefix+":total").Value("1").Build()).Error())

	b.Run("loyalty_warm_one_rtt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := bumpChannelScript.Exec(ctx, client, []string{prefix + ":counter"}, []string{"", "1", "60"}).Error(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("feed_warm_one_rtt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := decodeFeedCounts(feedWarmScript.Exec(ctx, client,
				[]string{prefix + ":total", prefix + ":today"}, []string{"60"})); err != nil {
				b.Fatal(err)
			}
		}
	})
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
