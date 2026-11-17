// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

func TestWatchMySQLFinalizationBetweenChunksStaysPending(t *testing.T) {
	repo, raw := mysqlRetentionRepo(t)
	hook := watchBeginHook(t, raw)
	intercepted := sql.OpenDB(hook)
	t.Cleanup(func() { intercepted.Close() })
	posting := watchRetentionRepo(t, intercepted, "mysql")
	a := bulkWatchAward(1, 1000)
	old := time.Now().Add(-9 * 24 * time.Hour)
	a.WindowStartedAtUnixMilli = old.UnixMilli()
	begins := 0
	hook.beforeBegin = func() error {
		begins++
		if begins != 2 {
			return nil
		}
		require.Equal(t, watchTotals{64, 640, 19200, 0, 64}, (watchBulkFixture{repo, raw}).totals(t))
		for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
			_, err := raw.ExecContext(t.Context(), "UPDATE "+table+" SET created_at=?", old.Unix())
			require.NoError(t, err)
		}
		return repo.PruneWatchHistory(t.Context(), time.Now().Add(-8*24*time.Hour).UnixMilli())
	}
	require.ErrorContains(t, posting.ApplyWatchAward(t.Context(), a), "finalized during incomplete award")
	hook.beforeBegin = nil
	for range 2 {
		require.ErrorContains(t, posting.ApplyWatchAward(t.Context(), a), "finalized during incomplete award")
		require.Equal(t, watchTotals{64, 640, 19200, 0, 0}, (watchBulkFixture{repo, raw}).totals(t))
	}
	var completed bool
	require.NoError(t, raw.QueryRowContext(t.Context(), "SELECT completed FROM loyalty_watch_operations").Scan(&completed))
	require.False(t, completed)
}

func TestWatchMySQLReplicasCheckPayloadUnderFence(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "same payload", true: "different payload"}[mismatch], func(t *testing.T) {
			repo, raw := mysqlRetentionRepo(t)
			awards := []data.WatchAwardDTO{bulkWatchAward(1, 1000), bulkWatchAward(1, 1000)}
			if mismatch {
				awards[1].Entries[0].ViewerName = "changed"
			}
			ready := make(chan struct{}, 2)
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, award := range awards {
				hook := watchBeginHook(t, raw)
				intercepted := sql.OpenDB(hook)
				t.Cleanup(func() { intercepted.Close() })
				replica := watchRetentionRepo(t, intercepted, "mysql")
				hook.beforeBegin = replicaWatchStart(ready, start)
				go func() { results <- replica.ApplyWatchAward(t.Context(), award) }()
			}
			for range 2 {
				select {
				case <-ready:
				case <-time.After(5 * time.Second):
					t.Fatal("replica did not reach its first transaction")
				}
			}
			close(start)
			failures := 0
			for range 2 {
				failures += watchReplicaResult(t, <-results)
			}
			require.Equal(t, map[bool]int{false: 0, true: 1}[mismatch], failures)
			require.Equal(t, watchTotals{1000, 10000, 300000, 1, 1000}, (watchBulkFixture{repo, raw}).totals(t))
		})
	}
}

func replicaWatchStart(ready chan<- struct{}, start <-chan struct{}) func() error {
	var once sync.Once
	return func() error {
		once.Do(func() { ready <- struct{}{}; <-start })
		return nil
	}
}

func watchReplicaResult(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	require.ErrorContains(t, err, "payload mismatch")
	return 1
}
