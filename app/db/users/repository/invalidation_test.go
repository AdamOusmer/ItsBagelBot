// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
)

func TestStatusChangesInvalidateAfterCommit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(context.Context, *repository.Users) error
		status user.Status
	}{
		{"status", func(ctx context.Context, r *repository.Users) error { return r.SetStatus(ctx, 1001, user.StatusVip) }, user.StatusVip},
		{"ban", func(ctx context.Context, r *repository.Users) error { return r.SetBanned(ctx, 1001, true) }, user.StatusPaid},
		{"admin", func(ctx context.Context, r *repository.Users) error {
			return r.SetAdminStatus(ctx, 1001, user.StatusVip, nil)
		}, user.StatusVip},
		{"lifecycle", func(ctx context.Context, r *repository.Users) error { return r.SetActiveNow(ctx, 1001, false) }, user.StatusPaid},
		{"expiry", func(ctx context.Context, r *repository.Users) error {
			_, err := r.ExpireSubscriptions(ctx, time.Now(), 0)
			return err
		}, user.StatusFree},
		{"billing", func(ctx context.Context, r *repository.Users) error {
			_, err := r.ApplyBilling(ctx, billingrpc.ApplyRequest{UserID: 1001, EventID: "billing", OccurredAt: time.Now(), Action: billingrpc.ActionActivate})
			return err
		}, user.StatusPaid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _, repo := setup(t)
			ctx := t.Context()
			require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1001, Username: "account", Email: "account@example.com"}))
			client.User.UpdateOneID(1001).SetStatus(user.StatusPaid).SetSubscriptionSource("admin").SetSubscriptionExpiresAt(time.Now().Add(-time.Hour)).ExecX(ctx)
			_, err := repo.UserStats(ctx)
			require.NoError(t, err)
			var committed []user.Status
			repo.Invalidations().Watch(invalidate.ScopeStatus, func(e cache.Event) {
				committed = append(committed, client.User.GetX(ctx, e.ID).Status)
			})
			require.NoError(t, tc.change(ctx, repo))
			require.Equal(t, []user.Status{tc.status}, committed)
			stats, err := repo.UserStats(ctx)
			require.NoError(t, err)
			require.Equal(t, client.User.Query().Where(user.StatusEQ(user.StatusPaid)).CountX(ctx), stats.Paid)
		})
	}
}

func TestRegistrationAndErasureRefreshStats(t *testing.T) {
	_, _, repo := setup(t)
	ctx := t.Context()
	_, err := repo.UserStats(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1001, Username: "account", Email: "account@example.com"}))
	stats, err := repo.UserStats(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Total)
	var scopes []string
	for _, scope := range []string{invalidate.ScopeStatus, invalidate.ScopeUser} {
		repo.Invalidations().Watch(scope, func(e cache.Event) { scopes = append(scopes, e.Scope) })
	}
	require.NoError(t, repo.Delete(ctx, 1001))
	require.Equal(t, []string{invalidate.ScopeStatus, invalidate.ScopeUser}, scopes)
	stats, err = repo.UserStats(ctx)
	require.NoError(t, err)
	require.Zero(t, stats.Total)
}

func TestErasureReplayInvalidatesStatus(t *testing.T) {
	client, _, repo := setup(t)
	store := newFakeErasureStore()
	store.objects["erasures/1001-111"] = time.Now().Add(-2 * time.Minute)
	client.LifecycleErasure.Create().SetUserID(1001).SetAccountCreatedAt(111).SaveX(t.Context())
	repo.SetErasureStore(store)
	var ids []uint64
	repo.Invalidations().Watch(invalidate.ScopeStatus, func(e cache.Event) { ids = append(ids, e.ID) })
	_, err := repo.ReplayErasures(t.Context(), time.Now())
	require.NoError(t, err)
	require.Equal(t, []uint64{1001}, ids)
}

func TestPreferenceInvalidationsFollowFlush(t *testing.T) {
	client, _, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1001, Username: "account", Email: "account@example.com"}))
	require.NoError(t, repo.SetCreatorCode(ctx, 1001, "account"))
	var committed []string
	for _, scope := range []string{invalidate.ScopeCursor, invalidate.ScopeLocale, invalidate.ScopeStatus} {
		repo.Invalidations().Watch(scope, func(e cache.Event) {
			row := client.User.GetX(ctx, e.ID)
			committed = append(committed, fmt.Sprintf("%s:%t:%s:%s", e.Scope, row.CustomCursor, row.Locale, *row.CreatorCode))
		})
	}
	require.NoError(t, repo.SetCustomCursor(ctx, 1001, false))
	require.NoError(t, repo.SetLocale(ctx, 1001, "fr"))
	require.NoError(t, repo.SetCreatorCode(ctx, 1001, "account"))
	repo.Close(ctx)
	require.ElementsMatch(t, []string{"status:false:en:account", "cursor:false:en:account", "status:false:fr:account", "locale:false:fr:account", "status:false:fr:account"}, committed)
}

func TestCommittedGrantReplayInvalidatesStatus(t *testing.T) {
	_, _, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1001, Username: "account", Email: "account@example.com"}))
	_, err := repo.PreparePremiumGrant(ctx, usersrpc.PreparePremiumGrantRequest{GiveawayID: "grant", AwardID: "award", UserID: 1001, StartAt: time.Now().Add(-time.Minute), EndAt: time.Now().Add(time.Hour), IntervalRuleVersion: "tebex-monthly-v1"})
	require.NoError(t, err)
	var ids []uint64
	repo.Invalidations().Watch(invalidate.ScopeStatus, func(e cache.Event) { ids = append(ids, e.ID) })
	for range 2 {
		_, err = repo.CommitPremiumGrant(ctx, usersrpc.CommitPremiumGrantRequest{GiveawayID: "grant", AwardID: "award", UserID: 1001})
		require.NoError(t, err)
	}
	require.Equal(t, []uint64{1001, 1001}, ids)
}

func TestReprojectPreservesLegacyLifecycleAndPageFlags(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, repository.Registration{ID: 1001, Username: "account", Email: "account@example.com"}))
	for _, page := range []repository.PublicPage{repository.PageCommands, repository.PageLeaderboard, repository.PageSongQueue} {
		require.NoError(t, repo.SetPageHidden(ctx, 1001, page, true))
	}
	client.User.UpdateOneID(1001).ClearLifecycle().ExecX(ctx)
	require.NoError(t, repo.Reproject(ctx))
	after := decodeChanged(t, pub)
	dto := after[len(after)-1]
	require.Nil(t, dto.Lifecycle)
	require.Equal(t, [3]bool{}, [3]bool{dto.CommandsPageHidden, dto.LeaderboardPageHidden, dto.SongQueuePageHidden})
}
