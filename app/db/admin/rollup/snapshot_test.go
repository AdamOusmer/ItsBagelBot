// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rollup_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/app/db/admin/rollup"
	catalog "ItsBagelBot/internal/domain/analytics"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
)

func annualFixture(t *testing.T) fixture {
	t.Helper()
	f := setup(t)
	f.now = time.Date(time.Now().Year()+1, time.January, 1, 2, 0, 0, 0, time.UTC)
	f.recordYesterday(t)
	return f
}

func TestRunArchivesFinalDayAfterFolding(t *testing.T) {
	f := annualFixture(t)
	ctx := context.Background()
	_, err := rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	snapshot, err := f.repo.AnalyticsSnapshot(ctx, f.now.Year()-1)
	require.NoError(t, err)

	values := map[catalog.Metric]int64{}
	for _, row := range snapshot.Payload.Rows {
		if row.Window != "d" {
			continue
		}
		values[row.Metric] += row.Value
	}
	require.Equal(t, int64(12), values[catalog.Lifecycle])
	require.Equal(t, int64(15), values[catalog.ChannelsActive])
	require.Equal(t, int64(3), values[catalog.AccountsTotal])
}

func TestRunRecoversPendingLegacyClaimBeforeArchiving(t *testing.T) {
	f := annualFixture(t)
	ctx := context.Background()
	yesterday := f.now.AddDate(0, 0, -1)
	_, err := pkgvalkey.ClaimOnce(ctx, f.vc, catalog.RollupClaimKey(catalog.Day, yesterday), 24*time.Hour)
	require.NoError(t, err)

	_, err = rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	snapshot, err := f.repo.AnalyticsSnapshot(ctx, f.now.Year()-1)
	require.NoError(t, err)

	var signups int64
	for _, row := range snapshot.Payload.Rows {
		if row.Metric == catalog.Lifecycle {
			signups += row.Value
		}
	}
	require.Equal(t, int64(12), signups)
}

func TestRunPreservesCompletedLegacyLevels(t *testing.T) {
	f := annualFixture(t)
	ctx := context.Background()
	yesterday := f.now.AddDate(0, 0, -1)
	_, err := rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)

	kv := pkgvalkey.NewKV(f.vc)
	require.NoError(t, kv.Del(ctx, catalog.RollupClaimKey(catalog.Day, yesterday)+":done"))
	_, err = pkgvalkey.ClaimOnce(ctx, f.vc, catalog.RollupClaimKey(catalog.Day, yesterday), 24*time.Hour)
	require.NoError(t, err)

	require.NoError(t, f.vc.Do(ctx, f.vc.B().Hset().Key(catalog.LevelKey).FieldValue().FieldValue("accounts.total|free", "999").Build()).Error())
	_, err = rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"accounts.total|free": 2, "accounts.total|paid": 1}, f.day(t, catalog.AccountsTotal))
}

type blockedStore struct {
	rollup.Store
	entered chan struct{}
	release chan struct{}
}

func (s blockedStore) UpsertAnalytics(ctx context.Context, rows []repository.AnalyticsRow) (int, error) {
	if len(rows) > 0 {
		select {
		case s.entered <- struct{}{}:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		select {
		case <-s.release:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return s.Store.UpsertAnalytics(ctx, rows)
}

func TestRunSerializesReplicaBeforeAnnualArchive(t *testing.T) {
	f := annualFixture(t)
	ctx := context.Background()
	slow := blockedStore{Store: f.repo, entered: make(chan struct{}, 1), release: make(chan struct{})}
	finished := make(chan error, 1)
	go func() {
		_, err := rollup.New(f.vc, slow).Run(ctx, f.now)
		finished <- err
	}()
	select {
	case <-slow.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first replica did not reach SQL write")
	}

	written, err := rollup.New(f.vc, f.repo).Run(ctx, f.now)
	require.NoError(t, err)
	require.Zero(t, written)
	_, err = f.repo.AnalyticsSnapshot(ctx, f.now.Year()-1)
	require.Error(t, err)

	close(slow.release)
	require.NoError(t, <-finished)
	snapshot, err := f.repo.AnalyticsSnapshot(ctx, f.now.Year()-1)
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Payload.Rows)
}
