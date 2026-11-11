// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/analyticsrollup"
	"ItsBagelBot/app/db/admin/ent/analyticssnapshot"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

func (r *Admin) ArchiveAnalytics(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC()
	boundary := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	if now.Before(boundary.Add(time.Minute)) {
		return 0, nil
	}
	first, err := db.WithRead(ctx, func(ctx context.Context) (*ent.AnalyticsRollup, error) {
		return r.client.AnalyticsRollup.Query().Where(analyticsrollup.PeriodStartLT(boundary)).
			Order(ent.Asc(analyticsrollup.FieldPeriodStart)).First(ctx)
	})
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	written := 0
	for year := first.PeriodStart.UTC().Year(); year < now.Year(); year++ {
		n, err := r.archiveAnalyticsYear(ctx, year, now)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (r *Admin) archiveAnalyticsYear(ctx context.Context, year int, now time.Time) (int, error) {
	exists, err := db.WithRead(ctx, func(ctx context.Context) (bool, error) {
		return r.client.AnalyticsSnapshot.Query().Where(analyticssnapshot.YearEQ(year)).Exist(ctx)
	})
	if err != nil || exists {
		return 0, err
	}
	payload, err := r.analyticsYear(ctx, year)
	if err != nil || len(payload.Rows) == 0 {
		return 0, err
	}
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.AnalyticsSnapshot.Create().SetYear(year).SetCapturedAt(now).SetPayload(payload).
			OnConflict(entsql.ConflictColumns(analyticssnapshot.FieldYear)).Ignore().Exec(ctx)
	})
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (r *Admin) analyticsYear(ctx context.Context, year int) (*catalog.Snapshot, error) {
	from := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.AnalyticsRollup, error) {
		return r.client.AnalyticsRollup.Query().Where(analyticsrollup.PeriodStartGTE(from), analyticsrollup.PeriodStartLT(to)).
			Order(ent.Asc(analyticsrollup.FieldPeriodStart, analyticsrollup.FieldWindow, analyticsrollup.FieldMetric,
				analyticsrollup.FieldD1, analyticsrollup.FieldD2, analyticsrollup.FieldD3)).All(ctx)
	})
	payload := &catalog.Snapshot{Version: 1, Rows: []catalog.SnapshotCell{}}
	for _, row := range rows {
		window, ok := catalog.ParseWindow(string(row.Window))
		if !ok || catalog.PeriodEnd(window, row.PeriodStart).After(to) {
			continue
		}
		payload.Rows = append(payload.Rows, catalog.SnapshotCell{
			Window: string(row.Window), Start: row.PeriodStart.UTC().Format(time.DateOnly),
			Metric: catalog.Metric(row.Metric), Dims: trimDims(row), Value: row.Value,
		})
	}
	return payload, err
}

func (r *Admin) AnalyticsSnapshot(ctx context.Context, year int) (*ent.AnalyticsSnapshot, error) {
	return db.WithRead(ctx, func(ctx context.Context) (*ent.AnalyticsSnapshot, error) {
		return r.client.AnalyticsSnapshot.Query().Where(analyticssnapshot.YearEQ(year)).Only(ctx)
	})
}
