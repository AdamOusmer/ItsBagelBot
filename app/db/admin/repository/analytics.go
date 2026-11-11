// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/analyticsrollup"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
	"context"
	entsql "entgo.io/ent/dialect/sql"
	"time"
)

type Admin struct{ client *ent.Client }

func New(client *ent.Client) *Admin { return &Admin{client: client} }

type AnalyticsRow struct {
	Window catalog.Window
	Start  time.Time
	Metric catalog.Metric
	Dims   []string
	Value  int64
}

func (r *Admin) UpsertAnalytics(ctx context.Context, rows []AnalyticsRow) (int, error) {
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

func (r *Admin) AnalyticsSeries(ctx context.Context, q SeriesQuery) ([]AnalyticsRow, error) {
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
