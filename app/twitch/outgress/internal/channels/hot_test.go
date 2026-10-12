package channels

import (
	"ItsBagelBot/internal/domain/rpc/manage"
	"context"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
	"time"
)

func TestHotRegistryErasureKeepsReplacementAccount(t *testing.T) {
	client := channelsRealValkey(t)
	r := New(client)
	defer r.Close()
	ctx := context.Background()
	require.NoError(t, r.Save(ctx, manage.Channel{BroadcasterID: "42", Enabled: true}))
	require.NoError(t, r.SetIncarnation(ctx, "42", 200))
	require.NoError(t, r.EraseIncarnation(ctx, "42", 100))
	ch, found, err := r.Get(ctx, "42")
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, ch.Enabled)
	require.NoError(t, r.EraseIncarnation(ctx, "42", 200))
	_, found, err = r.Get(ctx, "42")
	require.NoError(t, err)
	require.False(t, found)
}
func TestHotRegistryPrunePreservesRefreshedHashAndRemovesGhost(t *testing.T) {
	client := channelsRealValkey(t)
	r := New(client)
	defer r.Close()
	ctx := context.Background()
	require.NoError(t, r.Save(ctx, manage.Channel{BroadcasterID: "42", Enabled: true}))
	require.NoError(t, client.Do(ctx, client.B().Zadd().Key(hotIndexKey).ScoreMember().ScoreMember(float64(time.Now().Add(-time.Hour).Unix()), "42").Build()).Error())
	require.NoError(t, r.PruneHot(ctx))
	_, found, err := r.Get(ctx, "42")
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, client.Do(ctx, client.B().Del().Key(keyPrefix+"42").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Zadd().Key(hotIndexKey).ScoreMember().ScoreMember(float64(time.Now().Add(-time.Hour).Unix()), "42").Build()).Error())
	require.NoError(t, r.PruneHot(ctx))
	ids, err := client.Do(ctx, client.B().Zrange().Key(hotIndexKey).Min("0").Max(strconv.Itoa(hotBatch)).Build()).AsStrSlice()
	require.NoError(t, err)
	require.Empty(t, ids)
}
func TestHotRegistryListReturnsAllPagesAndHistoricalSetDrains(t *testing.T) {
	client := channelsRealValkey(t)
	r := New(client)
	defer r.Close()
	ctx := context.Background()
	for i := 0; i < hotBatch+1; i++ {
		require.NoError(t, r.Save(ctx, manage.Channel{BroadcasterID: strconv.Itoa(i + 1), Enabled: true}))
	}
	items, err := r.List(ctx)
	require.NoError(t, err)
	require.Len(t, items, hotBatch+1)
	for i := 0; i < 4; i++ {
		require.NoError(t, r.PruneHot(ctx))
	}
	count, err := client.Do(ctx, client.B().Scard().Key(indexKey).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, count)
}
