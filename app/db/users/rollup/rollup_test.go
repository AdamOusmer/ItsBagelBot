// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rollup_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rollup"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// now is a day ahead so yesterday's salts still live on the real Valkey clock.
type fixture struct {
	client *ent.Client
	repo   *repository.Users
	vc     valkey.Client
	now    time.Time
}

func setup(t *testing.T) fixture {
	t.Helper()
	client := testdb.Open(t, testdb.Name("rollup"+t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	return fixture{client: client, repo: repo, vc: valkeytest.Spawn(t), now: catalog.PeriodStart(catalog.Day, time.Now().AddDate(0, 0, 1)).Add(2 * time.Hour)}
}

func (f fixture) recordYesterday(t *testing.T) {
	t.Helper()
	yesterday := f.now.AddDate(0, 0, -1)
	rec := analytics.New(f.vc, zap.NewNop(), map[catalog.Kind][]string{catalog.KindCommand: {"followage"}},
		analytics.WithClock(func() time.Time { return yesterday }), analytics.WithoutTicker())
	for i := 0; i < 12; i++ {
		rec.Count(catalog.Lifecycle, "signup")
	}
	for id := uint64(1); id <= 15; id++ {
		rec.Distinct(catalog.ChannelsActive, id)
	}
	rec.Add(catalog.CommandUses, 40, "followage")
	rec.Max(catalog.LivePeak, 11)
	rec.Close()
}

func (f fixture) seed(t *testing.T) {
	t.Helper()
	for i, status := range []user.Status{user.StatusFree, user.StatusFree, user.StatusPaid} {
		f.client.User.Create().SetID(uint64(500 + i)).SetUsername(fmt.Sprint("u", i)).
			SetEmail(fmt.Sprintf("u%d@example.test", i)).SetStatus(status).SetSubscriptionSource("tebex").SaveX(context.Background())
	}
	f.client.User.Create().SetID(999).SetUsername("tester").SetEmail("t@example.test").SetTestAccount(true).SaveX(context.Background())
}

func (f fixture) day(t *testing.T, metric catalog.Metric) map[string]int64 {
	t.Helper()
	rows, err := f.repo.AnalyticsSeries(context.Background(), repository.SeriesQuery{
		Metrics: []catalog.Metric{metric},
		Window:  catalog.Day,
		From:    catalog.PeriodStart(catalog.Day, f.now.AddDate(0, 0, -1)),
		To:      f.now,
	})
	require.NoError(t, err)
	out := map[string]int64{}
	for _, row := range rows {
		out[catalog.Field(row.Metric, row.Dims...)] = row.Value
	}
	return out
}

func TestRunFoldsClosedDayAndClearsValkey(t *testing.T) {
	f := setup(t)
	f.seed(t)
	f.recordYesterday(t)
	ctx := context.Background()

	written, err := rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	require.Positive(t, written)

	require.Equal(t, map[string]int64{"lifecycle|signup": 12}, f.day(t, catalog.Lifecycle))
	require.Equal(t, map[string]int64{"command.uses|followage": 40}, f.day(t, catalog.CommandUses))
	require.Equal(t, map[string]int64{"live.peak": 11}, f.day(t, catalog.LivePeak))
	require.Equal(t, map[string]int64{"channels.active": 15}, f.day(t, catalog.ChannelsActive))
	require.Equal(t, map[string]int64{"accounts.total|free": 2, "accounts.total|paid": 1}, f.day(t, catalog.AccountsTotal))

	yesterday := f.now.AddDate(0, 0, -1)
	for _, key := range []string{catalog.CounterKey(yesterday), catalog.GaugeKey(yesterday), catalog.IndexKey(catalog.Day, yesterday)} {
		exists, err := f.vc.Do(ctx, f.vc.B().Exists().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		require.Zero(t, exists, key)
	}

	again, err := rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	require.Zero(t, again)
}

type failingStore struct{ rollup.Store }

func (failingStore) UpsertAnalytics(context.Context, []repository.AnalyticsRow) (int, error) {
	return 0, errors.New("db down")
}

func TestFailedFoldReleasesClaimForNextRun(t *testing.T) {
	f := setup(t)
	f.recordYesterday(t)
	ctx := context.Background()

	_, err := rollup.New(f.vc, failingStore{f.repo}).Run(ctx, f.now)
	require.Error(t, err)

	_, err = rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"lifecycle|signup": 12}, f.day(t, catalog.Lifecycle))
}

func TestUpsertDropsRowsOutsideCatalog(t *testing.T) {
	f := setup(t)
	yesterday := f.now.AddDate(0, 0, -1)
	n, err := f.repo.UpsertAnalytics(context.Background(), []repository.AnalyticsRow{
		{Window: catalog.Day, Start: yesterday, Metric: catalog.CommandUses, Dims: []string{"!so @someone"}, Value: 3},
		{Window: catalog.Day, Start: yesterday, Metric: "viewer.ids", Value: 1},
		{Window: catalog.Day, Start: yesterday, Metric: catalog.CommandUses, Dims: []string{"followage"}, Value: 2},
	})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, map[string]int64{"command.uses|followage": 2}, f.day(t, catalog.CommandUses))
}
