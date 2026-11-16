// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rollup_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/app/db/admin/rollup"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRunUsesLastSnapshotFromEachClosedDay(t *testing.T) {
	for _, delay := range []int{1, 2} {
		t.Run(map[int]string{1: "next day", 2: "delayed fold"}[delay], func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			snapshotDay := f.now.AddDate(0, 0, -delay)
			snapshotAt := catalog.PeriodEnd(catalog.Day, snapshotDay).Add(-15 * time.Minute)
			rec := analytics.New(f.vc, zap.NewNop(), nil, analytics.WithoutTicker())
			t.Cleanup(rec.Close)
			r := rollup.New(f.vc, f.repo)
			field := catalog.Field(catalog.AccountsTotal, "free")
			rec.Shift(nil, analytics.Tally{field: 100})
			rec.Flush(ctx)
			_, err := r.Run(ctx, snapshotAt.Add(-15*time.Minute))
			require.NoError(t, err)

			rec.Shift(analytics.Tally{field: 100}, analytics.Tally{field: 101})
			rec.Flush(ctx)
			_, err = r.Run(ctx, snapshotAt)
			require.NoError(t, err)
			assertSnapshot(t, f, snapshotDay, 101)

			rec.Shift(analytics.Tally{field: 101}, analytics.Tally{field: 111})
			rec.Flush(ctx)
			_, err = r.Run(ctx, f.now)
			require.NoError(t, err)
			assertSnapshot(t, f, f.now, 111)

			rows, err := f.repo.AnalyticsSeries(ctx, repository.SeriesQuery{
				Metrics: []catalog.Metric{catalog.AccountsTotal}, Window: catalog.Day,
				From: catalog.PeriodStart(catalog.Day, snapshotDay), To: catalog.PeriodEnd(catalog.Day, snapshotDay),
			})
			require.NoError(t, err)
			require.Equal(t, []repository.AnalyticsRow{{
				Window: catalog.Day, Start: catalog.PeriodStart(catalog.Day, snapshotDay),
				Metric: catalog.AccountsTotal, Dims: []string{"free"}, Value: 101,
			}}, rows)
		})
	}
}

func assertSnapshot(t *testing.T, f fixture, at time.Time, total int64) {
	t.Helper()
	ctx := context.Background()
	key := catalog.LevelSnapshotKey(at)
	fields, err := pkgvalkey.HashAllInts(ctx, f.vc, key)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"accounts.total|free": total}, fields)
	expiry, err := f.vc.Do(ctx, f.vc.B().Expiretime().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	require.Equal(t, catalog.DataExpiry(catalog.Day, at).Unix(), expiry)
}
