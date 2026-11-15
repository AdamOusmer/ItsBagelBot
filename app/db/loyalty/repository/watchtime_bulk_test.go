// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"math"
	"strconv"
	"strings"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

type watchBulkFixture struct {
	repo *loyaltyrepo.Loyalty
	raw  *sql.DB
}

func TestWatchBulkPosting(t *testing.T) {
	for name, open := range map[string]func(*testing.T) (*loyaltyrepo.Loyalty, *sql.DB){
		"sqlite": sqliteRetentionRepo,
		"mysql":  mysqlRetentionRepo,
	} {
		t.Run(name, func(t *testing.T) {
			for scenario, check := range map[string]func(*testing.T, watchBulkFixture){
				"replay and overlap": checkWatchBulkOverlap,
				"1000 viewers":       checkWatchBulkThousand,
				"overflow":           checkWatchBulkOverflow,
			} {
				t.Run(scenario, func(t *testing.T) {
					repo, raw := open(t)
					check(t, watchBulkFixture{repo: repo, raw: raw})
				})
			}
		})
	}
}

func bulkWatchAward(first, last uint64) data.WatchAwardDTO {
	award := watchAward(17, first, 100)
	award.Entries = nil
	for id := last; id >= first; id-- {
		entry := watchAward(17, id, 100).Entries[0]
		award.Entries = append(award.Entries, entry)
	}
	return award
}

type watchTotals struct {
	viewers, points, seconds int64
	operations, receipts     int64
}

func (f watchBulkFixture) totals(t *testing.T) watchTotals {
	t.Helper()
	var totals watchTotals
	require.NoError(t, f.raw.QueryRowContext(t.Context(), "SELECT COUNT(*),COALESCE(SUM(points),0),COALESCE(SUM(watch_seconds),0) FROM balances").Scan(&totals.viewers, &totals.points, &totals.seconds))
	for table, count := range map[string]*int64{
		"loyalty_watch_operations": &totals.operations,
		"loyalty_watch_viewers":    &totals.receipts,
	} {
		require.NoError(t, f.raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(count))
	}
	return totals
}

func checkWatchBulkOverlap(t *testing.T, f watchBulkFixture) {
	a := bulkWatchAward(1, 750)
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.Equal(t, watchTotals{750, 7500, 225000, 1, 750}, f.totals(t))

	b := bulkWatchAward(501, 1000)
	b.Chunk = 1
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), b))
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), b))
	require.Equal(t, watchTotals{1000, 10000, 300000, 2, 1000}, f.totals(t))

	a.WindowID = "next"
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.Equal(t, watchTotals{1000, 17500, 525000, 3, 1750}, f.totals(t))
	b.Entries[0].Points++
	require.ErrorContains(t, f.repo.ApplyWatchAward(t.Context(), b), "payload mismatch")
}

func checkWatchBulkThousand(t *testing.T, f watchBulkFixture) {
	a := bulkWatchAward(1, 1000)
	for i := range a.Entries {
		a.Entries[i].ViewerLogin = strings.Repeat("'", 64)
		a.Entries[i].ViewerName = strings.Repeat("\\", 64)
	}
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.Equal(t, watchTotals{1000, 10000, 300000, 1, 1000}, f.totals(t))

	a.WindowID = "duplicates"
	a.Entries[999] = a.Entries[0]
	a.Entries[999].Points = 99
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.Equal(t, watchTotals{1000, 19990, 599700, 2, 1999}, f.totals(t))
	row, found, err := f.repo.BalanceGet(t.Context(), a.UserID, 1000)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 20, row.Points)
}

func checkWatchBulkOverflow(t *testing.T, f watchBulkFixture) {
	a := bulkWatchAward(1, 1000)
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), watchAward(17, 1000, 100)))
	_, err := f.raw.ExecContext(t.Context(), "UPDATE balances SET points=?,watch_seconds=? WHERE user_id=17 AND viewer_id=1000", int64(math.MaxInt64-5), []byte(strconv.FormatUint(math.MaxUint64, 10)))
	require.NoError(t, err)
	a.WindowID = "overflow"
	require.ErrorIs(t, f.repo.ApplyWatchAward(t.Context(), a), loyaltyrepo.ErrInvalidInput)
	for _, table := range []string{"balances", "loyalty_watch_viewers", "loyalty_watch_operations"} {
		var count int
		require.NoError(t, f.raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Equal(t, 1, count)
	}
	_, err = f.raw.ExecContext(t.Context(), "UPDATE balances SET watch_seconds=0 WHERE user_id=17 AND viewer_id=1000")
	require.NoError(t, err)
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	row, found, err := f.repo.BalanceGet(t.Context(), a.UserID, 1000)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, struct {
		points  int64
		seconds uint64
	}{math.MaxInt64, 300}, struct {
		points  int64
		seconds uint64
	}{row.Points, row.WatchSeconds})
}
