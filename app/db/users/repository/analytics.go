// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/analyticsrollup"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

type AnalyticsRow struct {
	Window catalog.Window
	Start  time.Time
	Metric catalog.Metric
	Dims   []string
	Value  int64
}

func (r *Users) SetAnalytics(rec *analytics.Recorder) {
	r.analytics = rec
}

// trackAccounts reads the accounts' counted columns before a write and returns the step
// that shifts the running totals once it lands. A missing row tallies empty, so creates
// and deletes count as +/-.
func (r *Users) trackAccounts(ctx context.Context, ids ...uint64) func() {
	rec := r.analytics
	if rec == nil || len(ids) == 0 {
		return func() {}
	}
	before, err := r.accountState(ctx, ids)
	if err != nil {
		return func() {}
	}
	return func() {
		if after, err := r.accountState(ctx, ids); err == nil {
			rec.Shift(before, after)
		}
	}
}

// Must run outside db.WithExec: accountState takes its own gate slot.
func (r *Users) countCreated(ctx context.Context, id uint64) {
	if r.analytics == nil {
		return
	}
	if after, err := r.accountState(ctx, []uint64{id}); err == nil {
		r.analytics.Shift(nil, after)
	}
}

func (r *Users) accountState(ctx context.Context, ids []uint64) (analytics.Tally, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().
			Where(user.IDIn(ids...)).
			Select(user.FieldID, user.FieldStatus, user.FieldIsActive, user.FieldOnboarded, user.FieldSubscriptionSource,
				user.FieldSubscriptionCancelPending, user.FieldSubscriptionPaymentFailed, user.FieldLocale,
				user.FieldCommandsPageHidden, user.FieldLeaderboardPageHidden, user.FieldCreatedAt,
				user.FieldTestAccount, user.FieldBanned).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	tally := analytics.Tally{}
	for _, u := range rows {
		if !u.TestAccount && !u.Banned {
			tallyAccount(tally, u)
		}
	}
	return tally, nil
}

func tallyAccount(t analytics.Tally, u *ent.User) {
	tier := string(u.Status)
	week := catalog.Period(catalog.Week, u.CreatedAt)
	t.Add(catalog.AccountsTotal, tier)
	t.Add(catalog.AccountsBySignupWeek, week)
	t.Add(catalog.AccountsLocale, u.Locale)
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

// UpsertAnalytics drops rows the catalog does not declare before they reach the table.
func (r *Users) UpsertAnalytics(ctx context.Context, rows []AnalyticsRow) (int, error) {
	builders := make([]*ent.AnalyticsRollupCreate, 0, len(rows))
	for _, row := range rows {
		spec, ok := catalog.Lookup(row.Metric)
		if !ok || !catalog.Valid(row.Metric, spec.Type, row.Dims, nil) {
			continue
		}
		d := append(append([]string{}, row.Dims...), "", "", "")
		builders = append(builders, r.client.AnalyticsRollup.Create().
			SetWindow(analyticsrollup.Window(catalog.WindowCode(row.Window))).
			SetPeriodStart(catalog.PeriodStart(row.Window, row.Start)).
			SetMetric(string(row.Metric)).
			SetD1(d[0]).SetD2(d[1]).SetD3(d[2]).
			SetValue(row.Value))
	}
	if len(builders) == 0 {
		return 0, nil
	}
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.AnalyticsRollup.CreateBulk(builders...).
			OnConflict(entsql.ConflictColumns(
				analyticsrollup.FieldWindow, analyticsrollup.FieldPeriodStart, analyticsrollup.FieldMetric,
				analyticsrollup.FieldD1, analyticsrollup.FieldD2, analyticsrollup.FieldD3,
			)).
			UpdateValue().
			Exec(ctx)
	})
	return len(builders), err
}

type SeriesQuery struct {
	Metrics []catalog.Metric
	Window  catalog.Window
	From    time.Time
	To      time.Time
}

func (r *Users) AnalyticsSeries(ctx context.Context, q SeriesQuery) ([]AnalyticsRow, error) {
	names := make([]string, 0, len(q.Metrics))
	for _, m := range q.Metrics {
		names = append(names, string(m))
	}
	w := q.Window
	found, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.AnalyticsRollup, error) {
		return r.client.AnalyticsRollup.Query().
			Where(
				analyticsrollup.MetricIn(names...),
				analyticsrollup.WindowEQ(analyticsrollup.Window(catalog.WindowCode(w))),
				analyticsrollup.PeriodStartGTE(q.From),
				analyticsrollup.PeriodStartLT(q.To),
			).
			Order(ent.Asc(analyticsrollup.FieldPeriodStart)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]AnalyticsRow, 0, len(found))
	for _, row := range found {
		out = append(out, AnalyticsRow{Window: w, Start: row.PeriodStart, Metric: catalog.Metric(row.Metric), Dims: trimDims(row), Value: row.Value})
	}
	return out, nil
}

func trimDims(row *ent.AnalyticsRollup) []string {
	dims := []string{row.D1, row.D2, row.D3}
	for len(dims) > 0 && dims[len(dims)-1] == "" {
		dims = dims[:len(dims)-1]
	}
	return dims
}
