// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
)

var accountColumns = []string{
	user.FieldID, user.FieldStatus, user.FieldIsActive, user.FieldOnboarded, user.FieldSubscriptionSource,
	user.FieldSubscriptionCancelPending, user.FieldSubscriptionPaymentFailed, user.FieldLocale,
	user.FieldCommandsPageHidden, user.FieldLeaderboardPageHidden, user.FieldSongqueuePageHidden, user.FieldCreatedAt,
	user.FieldTestAccount, user.FieldBanned,
	user.FieldLifecycleState,
}

func tallyCounted(t analytics.Tally, u *ent.User) {
	if !u.TestAccount && !u.Banned {
		tallyAccount(t, u)
	}
}

func (r *Users) AnalyticsSeed(ctx context.Context) (analytics.Tally, error) {
	tally := analytics.Tally{}
	err := analytics.Pages(ctx,
		func(ctx context.Context, after *uint64, limit int) ([]*ent.User, error) {
			return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
				q := r.client.User.Query().Order(ent.Asc(user.FieldID)).Limit(limit)
				if after != nil {
					q = q.Where(user.IDGT(*after))
				}
				return q.Select(accountColumns...).All(ctx)
			})
		},
		func(u *ent.User) uint64 { return u.ID },
		func(u *ent.User) { tallyCounted(tally, u) },
	)
	if err != nil {
		return nil, err
	}
	if _, err := r.tallyDelegating(ctx, tally, nil); err != nil {
		return nil, err
	}
	return r.tallyDesktopGrants(ctx, tally)
}

func tallyAccount(t analytics.Tally, u *ent.User) {
	tier := string(u.Status)
	week := catalog.Period(catalog.Week, u.CreatedAt)
	t.Add(catalog.AccountsTotal, tier)
	t.Add(catalog.AccountsBySignupWeek, week)
	t.Add(catalog.AccountsLocale, u.Locale)
	t.Add(catalog.AccountsRuntime, u.LifecycleState)
	if u.IsActive {
		t.Add(catalog.AccountsBotEnabled, tier)
	}
	if u.Onboarded {
		t.Add(catalog.AccountsOnboarded)
	}
	if u.Status != user.StatusFree {
		t.Add(catalog.PaidBySignupWeek, week)
		t.Add(catalog.AccountsPaidSource, paidSource(u))
	}
	tallyFlags(t, u)
}

func tallyFlags(t analytics.Tally, u *ent.User) {
	flags := []struct {
		on     bool
		metric catalog.Metric
		dims   []string
	}{
		{u.SubscriptionCancelPending, catalog.AccountsCancelPending, nil},
		{u.SubscriptionPaymentFailed, catalog.AccountsPaymentFailed, nil},
		{u.CommandsPageHidden, catalog.AccountsPagesHidden, []string{"commands"}},
		{u.LeaderboardPageHidden, catalog.AccountsPagesHidden, []string{"leaderboard"}},
		{u.SongqueuePageHidden, catalog.AccountsPagesHidden, []string{"songqueue"}},
	}
	for _, f := range flags {
		if f.on {
			t.Add(f.metric, f.dims...)
		}
	}
}

func paidSource(u *ent.User) string {
	if u.Status == user.StatusVip {
		return "vip"
	}
	return u.SubscriptionSource
}
