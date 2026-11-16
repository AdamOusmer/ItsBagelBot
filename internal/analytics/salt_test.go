// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type saltClient struct {
	valkey.Client
	writes map[string]int
}

func (c *saltClient) Do(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	args := cmd.Commands()
	if args[0] == "SET" && strings.HasPrefix(args[1], "an:salt:") {
		c.writes[args[1]]++
	}
	return c.Client.Do(ctx, cmd)
}

func recordDistincts(r *analytics.Recorder, id uint64) {
	r.Distinct(catalog.ChannelsActive, id)
	r.Distinct(catalog.ChannelsLive, id)
	r.Distinct(catalog.ModuleActive, id, "quotes")
}

func TestFlushRevalidatesEachPeriodSaltOnce(t *testing.T) {
	ctx := context.Background()
	client := &saltClient{Client: valkeytest.Spawn(t), writes: map[string]int{}}
	at := day
	r := newRecorder(t, client, &at)

	for range 2 {
		recordDistincts(r, channel)
		r.Flush(ctx)
	}

	for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		require.Equal(t, 2, client.writes[catalog.SaltKey(window, at)])
		require.Equal(t, int64(1), scard(t, client, catalog.DistinctKey(window, at, string(catalog.ChannelsActive))))
	}
}

func TestPodsRevalidateLostOrChangedSalts(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "deleted", true: "replaced"}[changed], func(t *testing.T) {
			ctx := context.Background()
			client := valkeytest.Spawn(t)
			at := day
			podA := newRecorder(t, client, &at)
			recordDistincts(podA, channel)
			podA.Flush(ctx)
			resetSalts(t, client, changed)

			podB := newRecorder(t, client, &at)
			recordDistincts(podB, channel+1)
			podB.Flush(ctx)
			recordDistincts(podA, channel+1)
			podA.Flush(ctx)

			for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
				key := catalog.DistinctKey(window, at, string(catalog.ChannelsActive))
				require.Equal(t, int64(2), scard(t, client, key))
				expiry, err := client.Do(ctx, client.B().Expiretime().Key(catalog.SaltKey(window, at)).Build()).AsInt64()
				require.NoError(t, err)
				require.Equal(t, catalog.SaltExpiry(window, at).Unix(), expiry)
			}
		})
	}
}

func resetSalts(t *testing.T, client valkey.Client, changed bool) {
	t.Helper()
	ctx := context.Background()
	for _, window := range []catalog.Window{catalog.Day, catalog.Week, catalog.Month} {
		key := catalog.SaltKey(window, day)
		cmd := client.B().Del().Key(key).Build()
		if changed {
			cmd = client.B().Set().Key(key).Value(strings.Repeat("x", 32)).Exat(catalog.SaltExpiry(window, day)).Build()
		}
		require.NoError(t, client.Do(ctx, cmd).Error())
	}
}

func TestFlushSeparatesSaltsForBufferedPeriods(t *testing.T) {
	ctx := context.Background()
	client := &saltClient{Client: valkeytest.Spawn(t), writes: map[string]int{}}
	at := day
	r := newRecorder(t, client, &at)
	recordDistincts(r, channel)
	at = day.Add(24 * time.Hour)
	recordDistincts(r, channel)
	r.Flush(ctx)

	want := map[string]int{
		catalog.SaltKey(catalog.Day, day):   1,
		catalog.SaltKey(catalog.Day, at):    1,
		catalog.SaltKey(catalog.Week, day):  1,
		catalog.SaltKey(catalog.Month, day): 1,
	}
	require.Equal(t, want, client.writes)
	require.NotEqual(t, get(t, client, catalog.SaltKey(catalog.Day, day)), get(t, client, catalog.SaltKey(catalog.Day, at)))
}
