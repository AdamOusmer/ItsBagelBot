// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package watchtime_test

import (
	"ItsBagelBot/internal/watchtime"
	"context"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestProviderCooldownSharedMonotonicBoundedAndExpiring(t *testing.T) {
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR requires an isolated real Valkey")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	identity := "helix:bot:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	other := "helix:bot:" + strconv.FormatInt(time.Now().UnixNano()+42, 10)
	key := "watchtime:provider-reset:" + identity
	defer client.Do(ctx, client.B().Del().Key(key, "watchtime:provider-reset:"+other).Build())
	first, second := watchtime.NewStore(client), watchtime.NewStore(client)
	at, err := first.ProviderRetryAt(ctx, identity)
	require.NoError(t, err)
	require.True(t, at.IsZero())
	long := time.Now().Add(2 * time.Minute).Truncate(time.Millisecond)
	require.NoError(t, first.ObserveProviderReset(ctx, identity, long))
	require.NoError(t, second.ObserveProviderReset(ctx, identity, time.Now().Add(time.Minute)))
	require.NoError(t, second.ObserveProviderReset(ctx, identity, time.Now().Add(-time.Hour)))
	at, err = second.ProviderRetryAt(ctx, identity)
	require.NoError(t, err)
	require.Equal(t, long, at)
	at, err = second.ProviderRetryAt(ctx, other)
	require.NoError(t, err)
	require.True(t, at.IsZero(), "different provider tokens are isolated")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- first.ObserveProviderReset(ctx, identity, long.Add(time.Duration(i)*time.Second))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	at, err = second.ProviderRetryAt(ctx, identity)
	require.NoError(t, err)
	require.Equal(t, long.Add(7*time.Second), at)
	before := time.Now()
	require.NoError(t, first.ObserveProviderReset(ctx, identity, before.Add(48*time.Hour)))
	at, err = second.ProviderRetryAt(ctx, identity)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(24*time.Hour), at, time.Second)
	ttl, err := client.Do(ctx, client.B().Pttl().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, -1, ttl, "provider reset must survive volatile-lru eviction")
	require.NoError(t, client.Do(ctx, client.B().Del().Key(key).Build()).Error())
	require.NoError(t, first.ObserveProviderReset(ctx, identity, time.Now().Add(100*time.Millisecond)))
	require.Eventually(t, func() bool { at, err := second.ProviderRetryAt(ctx, identity); return err == nil && at.IsZero() }, time.Second, 20*time.Millisecond)
	require.NoError(t, client.Do(ctx, client.B().Set().Key(key).Value("broken").Build()).Error())
	_, err = first.ProviderRetryAt(ctx, identity)
	require.Error(t, err, "corrupt authority must not fail open")
	require.Error(t, first.ObserveProviderReset(ctx, identity, time.Now().Add(time.Minute)))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = second.ProviderRetryAt(cancelled, identity)
	require.Error(t, err, "authority unavailable must fail closed")
	require.Error(t, first.ObserveProviderReset(ctx, "helix:bot:0", time.Now().Add(time.Minute)))
}
