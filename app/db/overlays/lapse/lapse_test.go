// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package lapse_test

import (
	"context"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/enttest"
	"ItsBagelBot/app/db/overlays/lapse"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/objectstore/objectstoretest"
)

const (
	owner    = 1001
	retained = overlays.LapseRetentionDays * 24 * time.Hour
)

type plans map[uint64]bool

func (p plans) Premium(_ context.Context, userID uint64) (bool, error) { return p[userID], nil }

type fixture struct {
	repo    *repository.Overlays
	objects *objectstoretest.Memory
	plans   plans
	clock   time.Time
	sweeper *lapse.Sweeper
}

func setup(t *testing.T) *fixture {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	f := &fixture{objects: objectstoretest.NewMemory(), plans: plans{owner: true}, clock: time.Now()}
	f.repo = repository.New(client, bustest.NewPublisher(), f.objects, "https://cdn.itsbagelbot.com")
	f.sweeper = &lapse.Sweeper{Repo: f.repo, Plans: f.plans, Now: func() time.Time { return f.clock }}
	return f
}

func (f *fixture) seed(t *testing.T) {
	t.Helper()
	asset, err := f.repo.StoreAsset(t.Context(), repository.Asset{
		ID: "aaaa", UserID: owner, Type: overlays.AssetPNG, Bytes: 10, W: 2, H: 2, SHA256: strings.Repeat("a", 64),
	})
	require.NoError(t, err)
	require.NoError(t, f.objects.Put(t.Context(), objectstore.Object{Key: asset.Key(), Body: strings.NewReader("0123456789"), Size: 10}))
	doc := overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "o1", Name: "Mine", Width: 1920, Height: 1080,
		Widgets: []overlays.Widget{{
			ID: "w1", Name: "Logo", Kind: overlays.WidgetImage, W: 100, H: 100, Opacity: 1,
			Settings: map[string]any{"media": "asset:aaaa"},
		}},
		Assets: map[string]overlays.Asset{"aaaa": {}},
	}
	_, _, err = f.repo.CreateCustom(t.Context(), owner, doc)
	require.NoError(t, err)
	_, _, err = f.repo.AddTemplate(t.Context(), owner, string(overlays.TemplatePowered))
	require.NoError(t, err)
}

func (f *fixture) sweepAfter(t *testing.T, elapsed time.Duration, premium bool) {
	t.Helper()
	f.plans[owner] = premium
	f.clock = f.clock.Add(elapsed)
	require.NoError(t, f.sweeper.Sweep(t.Context()))
}

func (f *fixture) kinds(t *testing.T) []string {
	t.Helper()
	list, err := f.repo.ListOverlays(t.Context(), owner)
	require.NoError(t, err)
	kinds := make([]string, len(list))
	for i, item := range list {
		kinds[i] = item.Kind
	}
	return kinds
}

func TestLapseRetention(t *testing.T) {
	cases := []struct {
		name     string
		steps    []step
		wantKind []string
		wantKeys int
	}{
		{"premium account is untouched", []step{{retained * 2, true}}, []string{"custom", "system"}, 1},
		{"inside the retention period", []step{{0, false}, {retained - time.Hour, false}}, []string{"custom", "system"}, 1},
		{"deleted after the retention period", []step{{0, false}, {retained + time.Hour, false}}, []string{"system"}, 0},
		{"renewal restarts the period", []step{{0, false}, {retained / 2, true}, {retained, false}, {retained - time.Hour, false}}, []string{"custom", "system"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.seed(t)
			for _, s := range tc.steps {
				f.sweepAfter(t, s.elapsed, s.premium)
			}
			require.ElementsMatch(t, tc.wantKind, f.kinds(t))
			require.Len(t, f.objects.Keys(), tc.wantKeys)
		})
	}
}

type step struct {
	elapsed time.Duration
	premium bool
}

func TestSweepRepeatsAreIdempotent(t *testing.T) {
	f := setup(t)
	f.seed(t)

	f.sweepAfter(t, 0, false)
	f.sweepAfter(t, retained+time.Hour, false)
	f.sweepAfter(t, 0, false)

	require.Equal(t, []string{"system"}, f.kinds(t))
	require.Empty(t, f.objects.Keys())
}
