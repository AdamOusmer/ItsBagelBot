// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/repository"
	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/stretchr/testify/require"
)

func TestAnalyticsAnnualArchivePreservesClosedCells(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	december := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	lastDay := december.AddDate(0, 0, 30)
	boundary := december.AddDate(0, 1, 0)
	_, err := repo.UpsertAnalytics(ctx, []repository.AnalyticsRow{
		{Window: catalog.Month, Start: december, Metric: catalog.ChannelsActive, Value: 14},
		{Window: catalog.Day, Start: lastDay, Metric: catalog.AccountsTotal, Dims: []string{"paid"}, Value: 7},
		{Window: catalog.Day, Start: lastDay, Metric: catalog.ChatMessages, Value: 42},
		{Window: catalog.Week, Start: lastDay, Metric: catalog.ChannelsActive, Value: 18},
		{Window: catalog.Day, Start: boundary, Metric: catalog.ChatMessages, Value: 99},
	})
	require.NoError(t, err)
	now := boundary.Add(time.Minute)
	written, err := repo.ArchiveAnalytics(ctx, now)
	require.NoError(t, err)
	require.Equal(t, 1, written)

	snapshot, err := repo.AnalyticsSnapshot(ctx, 2026)
	require.NoError(t, err)
	require.Equal(t, now, snapshot.CapturedAt)
	require.Equal(t, &catalog.Snapshot{Version: 1, Rows: []catalog.SnapshotCell{
		{Window: "m", Start: "2026-12-01", Metric: catalog.ChannelsActive, Dims: []string{}, Value: 14},
		{Window: "d", Start: "2026-12-31", Metric: catalog.AccountsTotal, Dims: []string{"paid"}, Value: 7},
		{Window: "d", Start: "2026-12-31", Metric: catalog.ChatMessages, Dims: []string{}, Value: 42},
	}}, snapshot.Payload)

	_, err = repo.UpsertAnalytics(ctx, []repository.AnalyticsRow{
		{Window: catalog.Day, Start: lastDay, Metric: catalog.ChatMessages, Value: 1000},
	})
	require.NoError(t, err)
	written, err = repo.ArchiveAnalytics(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, written)

	again, err := repo.AnalyticsSnapshot(ctx, 2026)
	require.NoError(t, err)
	require.Equal(t, snapshot.Payload, again.Payload)
	require.Equal(t, snapshot.CapturedAt, again.CapturedAt)
}

func TestAnalyticsAnnualArchiveUsesUTCCutoff(t *testing.T) {
	cases := []struct {
		name string
		now  string
		want int
	}{
		{"before UTC new year", "2027-01-01T00:30:00+01:00", 0},
		{"waits for final flush", "2027-01-01T00:00:59Z", 0},
		{"UTC year closed", "2026-12-31T19:01:00-05:00", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := setup(t)
			ctx := context.Background()
			at := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)
			_, err := repo.UpsertAnalytics(ctx, []repository.AnalyticsRow{{Window: catalog.Day, Start: at, Metric: catalog.ChatMessages, Value: 1}})
			require.NoError(t, err)
			now, err := time.Parse(time.RFC3339, tc.now)
			require.NoError(t, err)

			written, err := repo.ArchiveAnalytics(ctx, now)
			require.NoError(t, err)
			require.Equal(t, tc.want, written)
		})
	}
}

func TestAnalyticsAnnualArchiveCatchesUpOnlyYearsWithData(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	now := time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)
	written, err := repo.ArchiveAnalytics(ctx, now)
	require.NoError(t, err)
	require.Zero(t, written)

	for _, year := range []int{2024, 2026} {
		at := time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
		_, err := repo.UpsertAnalytics(ctx, []repository.AnalyticsRow{{Window: catalog.Day, Start: at, Metric: catalog.ChatMessages, Value: int64(year)}})
		require.NoError(t, err)
	}
	written, err = repo.ArchiveAnalytics(ctx, now)
	require.NoError(t, err)
	require.Equal(t, 2, written)

	_, err = repo.AnalyticsSnapshot(ctx, 2025)
	require.True(t, ent.IsNotFound(err))
	_, err = repo.AnalyticsSnapshot(ctx, 2027)
	require.True(t, ent.IsNotFound(err))
}
