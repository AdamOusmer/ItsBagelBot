// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAccountLevelsFollowWrites(t *testing.T) {
	_, _, repo := setup(t)
	vc := valkeytest.Spawn(t)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
	repo.SetAnalytics(rec)
	ctx := context.Background()
	now := time.Now().UTC()

	for id := uint64(1); id <= 4; id++ {
		name := fmt.Sprint("user", id)
		require.NoError(t, repo.Register(ctx, id, name, name, name+"@example.test"))
	}
	_, err := repo.ApplyBilling(ctx, billingrpc.ApplyRequest{UserID: 1, EventID: "e1", Action: billingrpc.ActionActivate, OccurredAt: now, ExpiresAt: ptr(now.Add(time.Hour))})
	require.NoError(t, err)
	expires := now.Add(time.Hour)
	require.NoError(t, repo.SetAdminStatus(ctx, 2, user.StatusPaid, &expires))
	require.NoError(t, repo.SetLocaleNow(ctx, 3, "fr"))
	require.NoError(t, repo.SetCommandsPageHidden(ctx, 3, true))
	require.NoError(t, repo.SetOnboarded(ctx, 1, true))
	require.NoError(t, repo.Delete(ctx, 4))
	repo.Close(ctx)
	rec.Close()

	week := catalog.Period(catalog.Week, now)
	got, err := vc.Do(ctx, vc.B().Hgetall().Key(catalog.LevelKey).Build()).AsIntMap()
	require.NoError(t, err)
	for field, n := range got {
		if n == 0 {
			delete(got, field)
		}
	}
	require.Equal(t, map[string]int64{
		"accounts.total|free":                  1,
		"accounts.total|paid":                  2,
		"accounts.bot_enabled|free":            1,
		"accounts.bot_enabled|paid":            2,
		"accounts.onboarded":                   1,
		"accounts.paid_source|tebex":           1,
		"accounts.paid_source|admin":           1,
		"accounts.locale|en":                   2,
		"accounts.locale|fr":                   1,
		"accounts.pages_hidden|commands":       1,
		"accounts.by_signup_week|" + week:      3,
		"accounts.paid_by_signup_week|" + week: 2,
	}, got)
}

func TestAnalyticsSeedTalliesCountedAccounts(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	now := time.Now().UTC()

	create := func(id uint64) *ent.UserCreate {
		name := fmt.Sprint("seed", id)
		return client.User.Create().SetID(id).SetUsername(name).SetEmail(name + "@example.test")
	}
	require.NoError(t, create(1).Exec(ctx))
	require.NoError(t, create(2).SetStatus(user.StatusPaid).SetSubscriptionSource("tebex").SetSubscriptionCancelPending(true).
		SetOnboarded(true).SetIsActive(false).Exec(ctx))
	require.NoError(t, create(3).SetStatus(user.StatusVip).SetLocale("fr").SetCommandsPageHidden(true).
		SetLeaderboardPageHidden(true).SetSubscriptionPaymentFailed(true).Exec(ctx))
	require.NoError(t, create(4).SetTestAccount(true).SetStatus(user.StatusPaid).Exec(ctx))
	require.NoError(t, create(5).SetBanned(true).Exec(ctx))

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)

	week := catalog.Period(catalog.Week, now)
	require.Equal(t, analytics.Tally{
		"accounts.total|free":                  1,
		"accounts.total|paid":                  1,
		"accounts.total|vip":                   1,
		"accounts.bot_enabled|free":            1,
		"accounts.bot_enabled|vip":             1,
		"accounts.onboarded":                   1,
		"accounts.paid_source|tebex":           1,
		"accounts.paid_source|vip":             1,
		"accounts.cancel_pending":              1,
		"accounts.payment_failed":              1,
		"accounts.locale|en":                   2,
		"accounts.locale|fr":                   1,
		"accounts.pages_hidden|commands":       1,
		"accounts.pages_hidden|leaderboard":    1,
		"accounts.by_signup_week|" + week:      3,
		"accounts.paid_by_signup_week|" + week: 2,
	}, got)
}

func TestAnalyticsSeedPagesPastOnePage(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	const total = 2300

	for start := 1; start <= total; start += 500 {
		builders := make([]*ent.UserCreate, 0, 500)
		for id := start; id < start+500 && id <= total; id++ {
			name := fmt.Sprint("bulk", id)
			builders = append(builders, client.User.Create().SetID(uint64(id)).SetUsername(name).SetEmail(name+"@example.test"))
		}
		require.NoError(t, client.User.CreateBulk(builders...).Exec(ctx))
	}

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.EqualValues(t, total, got["accounts.total|free"])
	require.EqualValues(t, total, got["accounts.locale|en"])
}

func TestAnalyticsSeedOnceWritesLevelsOnce(t *testing.T) {
	client, _, repo := setup(t)
	vc := valkeytest.Spawn(t)
	rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(rec.Close)
	ctx := context.Background()
	require.NoError(t, client.User.Create().SetID(1).SetUsername("seed1").SetEmail("seed1@example.test").Exec(ctx))
	owned := []string{string(catalog.AccountsTotal)}

	rec.SeedOnce(ctx, "users", owned, repo.AnalyticsSeed)
	got, err := vc.Do(ctx, vc.B().Hget().Key(catalog.LevelKey).Field("accounts.total|free").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, got)

	ran := false
	rec.SeedOnce(ctx, "users", owned, func(context.Context) (analytics.Tally, error) {
		ran = true
		return analytics.Tally{}, nil
	})
	require.False(t, ran)
}

func ptr[T any](v T) *T { return &v }
