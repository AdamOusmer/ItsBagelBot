// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rollup_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/app/db/admin/rollup"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRunSnapshotsOwnerRPCReplies(t *testing.T) {
	f := setup(t)
	nc := testnats.Connect(t)
	for _, owner := range []struct {
		service rpc.LevelService
		field   string
	}{
		{rpc.UsersLevels, "accounts.onboarded"}, {rpc.ModulesLevels, "recipes.total|event|on"}, {rpc.CommandsLevels, "custom.channels"},
	} {
		source := analytics.NewLevelSource(analytics.Owner{Service: owner.service, Owned: analytics.Owned{owner.field}, Seed: func(context.Context) (analytics.Tally, error) { return analytics.Tally{owner.field: 12}, nil }})
		require.NoError(t, source.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop()}))
	}
	snapshot := pkgvalkey.IntHashSnapshot{Key: catalog.LevelSnapshotKey(f.now), Values: map[string]int64{"stale": 99}, ExpireAt: catalog.DataExpiry(catalog.Day, f.now)}
	require.NoError(t, snapshot.Replace(t.Context(), f.vc))
	_, err := rollup.New(f.vc, f.repo, analytics.RequestLevels(nc)).Run(t.Context(), f.now)
	require.NoError(t, err)
	got, err := pkgvalkey.HashAllInts(t.Context(), f.vc, snapshot.Key)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"accounts.onboarded": 12, "recipes.total|event|on": 12, "custom.channels": 12}, got)
}

func TestRunStopsBeforeSnapshotAndFoldOnOwnerFailure(t *testing.T) {
	f := setup(t)
	f.recordYesterday(t)
	want := errors.New("owner SQL unavailable")
	_, err := rollup.New(f.vc, f.repo, func(context.Context) (analytics.Tally, error) { return nil, want }).Run(t.Context(), f.now)
	require.ErrorIs(t, err, want)
	require.Empty(t, f.day(t, catalog.Lifecycle))
	got, err := pkgvalkey.HashAllInts(t.Context(), f.vc, catalog.LevelSnapshotKey(f.now))
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRunUsesLastSnapshotFromEachClosedDay(t *testing.T) {
	for _, delay := range []int{1, 2} {
		t.Run(map[int]string{1: "next day", 2: "delayed fold"}[delay], func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			snapshotDay := f.now.AddDate(0, 0, -delay)
			snapshotAt := catalog.PeriodEnd(catalog.Day, snapshotDay).Add(-15 * time.Minute)
			total := int64(100)
			r := rollup.New(f.vc, f.repo, func(context.Context) (analytics.Tally, error) {
				return analytics.Tally{"accounts.total|free": total}, nil
			})
			_, err := r.Run(ctx, snapshotAt.Add(-15*time.Minute))
			require.NoError(t, err)

			total = 101
			_, err = r.Run(ctx, snapshotAt)
			require.NoError(t, err)
			assertSnapshot(t, f, snapshotDay, 101)

			total = 111
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
