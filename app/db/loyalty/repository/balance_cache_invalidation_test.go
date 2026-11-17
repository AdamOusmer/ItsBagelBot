// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"slices"
	"strconv"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type balanceCacheRecorder struct {
	valkey.Client
	commands [][]string
}

func (c *balanceCacheRecorder) Do(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	c.commands = append(c.commands, slices.Clone(cmd.Commands()))
	return c.Client.Do(ctx, cmd)
}

func TestBalanceCacheDropsOnceBeforeInvalidation(t *testing.T) {
	for _, account := range []bool{false, true} {
		t.Run(strconv.FormatBool(account), func(t *testing.T) {
			repo, _ := sqliteRetentionRepo(t)
			require.NoError(t, repo.RestoreUser(t.Context(), 17, 100))
			client := &balanceCacheRecorder{Client: valkeytest.Spawn(t)}
			repo.SetBalanceCache(client)
			keys := []string{"loyal:bal:17:7", "loyal:bal:170:7", "loyal:cnt:c:17:hugs", "loyal:cnt:v:17:hugs", "loyal:cnt:c:170:17"}
			for _, key := range keys {
				require.NoError(t, client.Do(t.Context(), client.B().Set().Key(key).Value("5").Build()).Error())
			}
			h := bus.NewInvalidations(nil, "", nil)
			h.Watch(invalidate.ScopeLoyalty, func(e cache.Event) {
				present, err := client.Do(t.Context(), client.B().Exists().Key(keys[0]).Build()).AsInt64()
				require.NoError(t, err)
				require.Zero(t, present)
			})
			repo.SetInvalidations(h)
			client.commands = nil
			if account {
				require.NoError(t, repo.DeleteAccount(t.Context(), 17, 100))
			} else {
				_, _, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 17, ViewerID: 7, ViewerLogin: "viewer", Value: 1})
				require.NoError(t, err)
			}
			assertBalanceCacheWork(t, client.commands, account)
			assertRetainedBalanceKeys(t, client, keys, account)
		})
	}
}

func assertRetainedBalanceKeys(t *testing.T, client valkey.Client, keys []string, account bool) {
	t.Helper()
	removed := map[int]bool{0: true, 2: account, 3: account}
	for i, key := range keys {
		present, err := client.Do(t.Context(), client.B().Exists().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		require.Equal(t, !removed[i], present != 0, key)
	}
}

func assertBalanceCacheWork(t *testing.T, commands [][]string, account bool) {
	t.Helper()
	var unlinks, scanStarts int
	for _, command := range commands {
		if command[0] == "UNLINK" {
			unlinks++
		}
		if command[0] == "SCAN" && command[1] == "0" {
			scanStarts++
		}
	}
	wantScans := 0
	if account {
		wantScans = 1
	}
	require.Equal(t, []int{1, wantScans}, []int{unlinks, scanStarts})
}

func TestWatchChunksDropSharedBalancesBeforeInvalidation(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	client := &balanceCacheRecorder{Client: valkeytest.Spawn(t)}
	repo.SetBalanceCache(client)
	for id := 1; id <= 130; id++ {
		key := cache.PairKey("loyal:bal:", 17, strconv.Itoa(id))
		require.NoError(t, client.Do(t.Context(), client.B().Set().Key(key).Value("stale").Build()).Error())
	}
	h := bus.NewInvalidations(nil, "", nil)
	var sizes []int
	h.Watch(invalidate.ScopeLoyalty, func(e cache.Event) {
		keys := make([]string, 0, len(e.Keys))
		for _, id := range e.Keys {
			keys = append(keys, cache.PairKey("loyal:bal:", e.ID, id))
		}
		present, err := client.Do(t.Context(), client.B().Exists().Key(keys...).Build()).AsInt64()
		require.NoError(t, err)
		require.Zero(t, present)
		sizes = append(sizes, len(e.Keys))
	})
	repo.SetInvalidations(h)
	client.commands = nil
	award := bulkWatchAward(1, 130)
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	require.Equal(t, []int{64, 64, 2}, sizes)
	var unlinks []int
	for _, command := range client.commands {
		if command[0] == "UNLINK" {
			unlinks = append(unlinks, len(command)-1)
		}
	}
	require.Equal(t, sizes, unlinks)
	client.commands = nil
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	require.Empty(t, client.commands)
}
