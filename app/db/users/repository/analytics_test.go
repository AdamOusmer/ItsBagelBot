// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

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

func ptr[T any](v T) *T { return &v }
