// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// MYSQL_TEST_DSN must point at a disposable test server with CREATE DATABASE
// permission. Tests use a unique database, never the DSN's existing database.
func mysqlRetentionRepo(t *testing.T) (*loyaltyrepo.Loyalty, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run isolated real-MySQL watchtime tests")
	}
	cfg, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 20 * time.Second
	cfg.WriteTimeout = 20 * time.Second
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, admin.PingContext(ctx))
	name := "bagel_watch_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, err := admin.ExecContext(cleanup, "DROP DATABASE `"+name+"`")
		if err != nil {
			t.Errorf("drop temporary watchtime database: %v", err)
		}
	})
	cfg.DBName = name
	raw, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	raw.SetMaxOpenConns(24)
	t.Cleanup(func() { raw.Close() })
	return watchRetentionRepo(t, raw, "mysql"), raw
}

func TestWatchMySQLRetentionRejectsReplayAfterPruning(t *testing.T) {
	r, raw := mysqlRetentionRepo(t)
	checkWatchRetentionReplay(t, r, raw)
}

func TestWatchMySQLConcurrentDuplicateChunksAndTenants(t *testing.T) {
	r, _ := mysqlRetentionRepo(t)
	ctx := t.Context()
	const tenants, viewers, duplicates = 4, 1000, 4
	windowStart := time.Now().UnixMilli()
	start := make(chan struct{})
	errs := make(chan error, tenants*duplicates)
	latencies := make(chan time.Duration, tenants*duplicates)
	var workers sync.WaitGroup
	for tenant := 0; tenant < tenants; tenant++ {
		for copy := 0; copy < duplicates; copy++ {
			workers.Add(1)
			go func(user uint64, chunk uint32) {
				defer workers.Done()
				<-start
				a := watchAward(user, 1, 100)
				a.WindowStartedAtUnixMilli = windowStart
				a.Chunk = chunk
				a.Entries = a.Entries[:0]
				for v := 1; v <= viewers; v++ {
					e := watchAward(user, uint64(v), 100).Entries[0]
					e.ViewerLogin = fmt.Sprintf("viewer%d", v)
					a.Entries = append(a.Entries, e)
				}
				// Match the production consumer's complete SQL posting budget,
				// including contention behind other chunks for this tenant.
				posting, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				began := time.Now()
				errs <- r.ApplyWatchAward(posting, a)
				latencies <- time.Since(began)
			}(uint64(100+tenant), uint32(copy%2))
		}
	}
	began := time.Now()
	close(start)
	workers.Wait()
	close(errs)
	close(latencies)
	for err := range errs {
		require.NoError(t, err)
	}
	var maximum time.Duration
	for elapsed := range latencies {
		maximum = max(maximum, elapsed)
	}
	t.Logf("posted %d concurrent 1000-viewer chunks across %d tenants: total=%s max_commit=%s budget=10s", tenants*duplicates, tenants, time.Since(began), maximum)
	require.Less(t, maximum, 10*time.Second)
	for tenant := 0; tenant < tenants; tenant++ {
		for _, viewer := range []uint64{1, 500, 1000} {
			row, found, err := r.BalanceGet(ctx, uint64(100+tenant), viewer)
			require.NoError(t, err)
			require.True(t, found)
			require.EqualValues(t, 10, row.Points)
			require.EqualValues(t, 300, row.WatchSeconds)
		}
	}
}

func TestWatchMySQLPostingSerializesWithRetentionAndDeletion(t *testing.T) {
	r, raw := mysqlRetentionRepo(t)
	ctx := t.Context()
	a := watchAward(74, 84, 100)
	a.WindowStartedAtUnixMilli = time.Now().Add(-9 * 24 * time.Hour).UnixMilli()
	require.NoError(t, r.RestoreUser(ctx, a.UserID, a.AccountCreatedAt))
	start := make(chan struct{})
	errs := make(chan error, 3)
	var workers sync.WaitGroup
	for _, action := range []func() error{
		func() error { return r.ApplyWatchAward(ctx, a) },
		func() error { return r.PruneWatchHistory(ctx, time.Now().Add(-8*24*time.Hour).UnixMilli()) },
		func() error { return r.DeleteAccount(ctx, a.UserID, a.AccountCreatedAt) },
	} {
		workers.Add(1)
		go func(action func() error) { defer workers.Done(); <-start; errs <- action() }(action)
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	_, found, err := r.BalanceGet(ctx, a.UserID, 84)
	require.NoError(t, err)
	require.False(t, found)
	var retired int64
	require.NoError(t, raw.QueryRowContext(ctx, "SELECT finalized_window_ms FROM loyalty_watch_retention WHERE id=1").Scan(&retired))
	require.Greater(t, retired, a.WindowStartedAtUnixMilli)
}

func TestWatchMySQLRecreationAndLegacyFlushRace(t *testing.T) {
	r, _ := mysqlRetentionRepo(t)
	ctx := t.Context()
	old := watchAward(77, 87, 100)
	old.WindowStartedAtUnixMilli = time.Now().UnixMilli()
	require.NoError(t, r.RestoreUser(ctx, old.UserID, old.AccountCreatedAt))
	r.RecordEarned(data.LoyaltyEarnedDTO{UserID: old.UserID, Entries: old.Entries})
	concurrent := func(actions ...func() error) {
		t.Helper()
		start := make(chan struct{})
		errs := make(chan error, len(actions))
		var workers sync.WaitGroup
		for _, action := range actions {
			workers.Add(1)
			go func(action func() error) {
				defer workers.Done()
				<-start
				errs <- action()
			}(action)
		}
		close(start)
		workers.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
	}
	post := func(a data.WatchAwardDTO) error {
		posting, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return r.ApplyWatchAward(posting, a)
	}
	concurrent(
		func() error { r.Flush(ctx); return nil },
		func() error { return r.DeleteAccount(ctx, old.UserID, old.AccountCreatedAt) },
		func() error { return post(old) },
		func() error {
			r.RecordEarned(data.LoyaltyEarnedDTO{UserID: old.UserID, Entries: old.Entries})
			return nil
		},
	)
	r.Flush(ctx)
	_, found, err := r.BalanceGet(ctx, old.UserID, 87)
	require.NoError(t, err)
	require.False(t, found, "retirement removes committed and buffered old-account credits")
	require.NoError(t, r.RestoreUser(ctx, old.UserID, 101))
	fresh := old
	fresh.AccountCreatedAt = 101
	concurrent(
		func() error { return r.DeleteAccount(ctx, old.UserID, old.AccountCreatedAt) },
		func() error { return r.RestoreUser(ctx, old.UserID, old.AccountCreatedAt) },
		func() error { return post(old) },
		func() error { return post(fresh) },
		func() error {
			r.RecordEarned(data.LoyaltyEarnedDTO{UserID: fresh.UserID, Entries: []data.LoyaltyEarnEntry{{ViewerID: 87, ViewerLogin: "viewer", Points: 5}}})
			r.Flush(ctx)
			return nil
		},
	)
	r.Flush(ctx)
	require.NoError(t, post(fresh), "duplicate fresh award is idempotent")
	row, found, err := r.BalanceGet(ctx, fresh.UserID, 87)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 15, row.Points)
	require.EqualValues(t, 300, row.WatchSeconds)
}
