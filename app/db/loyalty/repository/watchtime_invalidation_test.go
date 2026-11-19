// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"slices"
	"strconv"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
)

func TestWatchChunksInvalidateCommittedAdmissions(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		trigger string
		before  int
	}{
		{name: "success", before: 3},
		{name: "first chunk rollback", trigger: "INSERT ON balances WHEN NEW.viewer_id=1"},
		{name: "second chunk rollback", trigger: "INSERT ON balances WHEN NEW.viewer_id=65", before: 1},
		{name: "completion rollback", trigger: "UPDATE ON loyalty_watch_operations", before: 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo, raw := sqliteRetentionRepo(t)
			events := watchCommittedInvalidations(t, repo)
			award := bulkWatchAward(1, 130)
			if scenario.trigger != "" {
				_, err := raw.Exec("CREATE TRIGGER abort_watch BEFORE " + scenario.trigger + " BEGIN SELECT RAISE(ABORT, 'chunk abort'); END")
				require.NoError(t, err)
				require.ErrorContains(t, repo.ApplyWatchAward(t.Context(), award), "chunk abort")
				require.Len(t, *events, scenario.before)
				_, err = raw.Exec("DROP TRIGGER abort_watch")
				require.NoError(t, err)
			}
			require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
			assertWatchChunkEvents(t, *events)
			require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
			overlap := bulkWatchAward(1, 130)
			overlap.Chunk++
			require.NoError(t, repo.ApplyWatchAward(t.Context(), overlap))
			assertWatchChunkEvents(t, *events)
		})
	}
}

func watchCommittedInvalidations(t *testing.T, repo *loyaltyrepo.Loyalty) *[]cache.Event {
	t.Helper()
	events := new([]cache.Event)
	h := bus.NewInvalidations(nil, "", nil)
	h.Watch(invalidate.ScopeLoyalty, func(e cache.Event) {
		for _, key := range e.Keys {
			id, err := strconv.ParseUint(key, 10, 64)
			require.NoError(t, err)
			row, found, err := repo.BalanceGet(t.Context(), e.ID, id)
			require.NoError(t, err)
			require.True(t, found)
			require.EqualValues(t, 10, row.Points)
		}
		*events = append(*events, e)
	})
	repo.SetInvalidations(h)
	return events
}

func assertWatchChunkEvents(t *testing.T, events []cache.Event) {
	t.Helper()
	require.Len(t, events, 3)
	for i, event := range events {
		keys := make([]string, 0, 64)
		for id := i*64 + 1; id <= min((i+1)*64, 130); id++ {
			keys = append(keys, strconv.Itoa(id))
		}
		require.Equal(t, cache.Event{Scope: invalidate.ScopeLoyalty, ID: 17, Keys: keys}, event)
	}
}

func TestWatchOverlapInvalidatesOnlyNewViewers(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	require.NoError(t, repo.ApplyWatchAward(t.Context(), bulkWatchAward(1, 70)))
	events := captureInvalidations(repo)
	award := bulkWatchAward(1, 80)
	award.Chunk++
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	keys := make([]string, 0, 10)
	for id := 71; id <= 80; id++ {
		keys = append(keys, strconv.Itoa(id))
	}
	slices.Sort(keys)
	require.Equal(t, []cache.Event{{Scope: invalidate.ScopeLoyalty, ID: 17, Keys: keys}}, *events)
}

func TestFinalizedWatchWindowDoesNotInvalidate(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	cutoff := time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	require.NoError(t, repo.PruneWatchHistory(t.Context(), cutoff))
	events := captureInvalidations(repo)
	award := watchAward(17, 7, 100)
	award.WindowStartedAtUnixMilli = cutoff
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	require.Empty(t, *events)
}
