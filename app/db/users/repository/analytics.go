// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/analyticsrollup"
	"ItsBagelBot/app/db/users/ent/user"
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

type analyticsTally map[string]*AnalyticsRow

func (t analyticsTally) add(start time.Time, m catalog.Metric, dims ...string) {
	key := catalog.Field(m, dims...)
	if row, ok := t[key]; ok {
		row.Value++
		return
	}
	t[key] = &AnalyticsRow{Window: catalog.Day, Start: start, Metric: m, Dims: dims, Value: 1}
}

func (t analyticsTally) rows() []AnalyticsRow {
	out := make([]AnalyticsRow, 0, len(t))
	for _, row := range t {
		out = append(out, *row)
	}
	return out
}

// AnalyticsSnapshot counts accounts as they stand now and files them under day.
func (r *Users) AnalyticsSnapshot(ctx context.Context, day time.Time) ([]AnalyticsRow, error) {
	accounts, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().
			Where(user.TestAccountEQ(false), user.BannedEQ(false)).
			Select(user.FieldStatus, user.FieldIsActive, user.FieldOnboarded, user.FieldSubscriptionSource,
				user.FieldSubscriptionCancelPending, user.FieldSubscriptionPaymentFailed, user.FieldLocale,
				user.FieldCommandsPageHidden, user.FieldLeaderboardPageHidden, user.FieldCreatedAt).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	start := catalog.PeriodStart(catalog.Day, day)
	tally := analyticsTally{}
	for _, u := range accounts {
		tallyAccount(tally, start, u)
	}
	return tally.rows(), nil
}

func tallyAccount(t analyticsTally, start time.Time, u *ent.User) {
	tier := string(u.Status)
	week := catalog.Period(catalog.Week, u.CreatedAt)
	t.add(start, catalog.AccountsTotal, tier)
	t.add(start, catalog.AccountsBySignupWeek, week)
	t.add(start, catalog.AccountsLocale, u.Locale)
	if u.IsActive {
		t.add(start, catalog.AccountsBotEnabled, tier)
	}
	if u.Onboarded {
		t.add(start, catalog.AccountsOnboarded)
	}
	if u.Status != user.StatusFree {
		t.add(start, catalog.PaidBySignupWeek, week)
		t.add(start, catalog.AccountsPaidSource, paidSource(u))
	}
	tallyFlags(t, start, u)
}

func tallyFlags(t analyticsTally, start time.Time, u *ent.User) {
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
			t.add(start, f.metric, f.dims...)
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

func (r *Users) AnalyticsSeries(ctx context.Context, metrics []catalog.Metric, w catalog.Window, from, to time.Time) ([]AnalyticsRow, error) {
	names := make([]string, 0, len(metrics))
	for _, m := range metrics {
		names = append(names, string(m))
	}
	found, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.AnalyticsRollup, error) {
		return r.client.AnalyticsRollup.Query().
			Where(
				analyticsrollup.MetricIn(names...),
				analyticsrollup.WindowEQ(analyticsrollup.Window(catalog.WindowCode(w))),
				analyticsrollup.PeriodStartGTE(from),
				analyticsrollup.PeriodStartLT(to),
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
