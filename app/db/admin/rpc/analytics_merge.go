// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"cmp"
	"context"
	"slices"
	"time"

	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
)

func (a *adminRPC) analyticsSeries(ctx context.Context, q repository.SeriesQuery) ([]repository.AnalyticsRow, error) {
	liveQuery := analytics.Query{Metrics: q.Metrics, Window: q.Window, From: q.From, Now: q.To}
	live, err := a.live.Read(ctx, liveQuery)
	if err != nil {
		return nil, err
	}
	stored, err := a.repo.AnalyticsSeries(ctx, q)
	if err != nil {
		return nil, err
	}
	if a.live == nil {
		return stored, nil
	}
	return mergeAnalytics(live, stored, liveQuery), nil
}

type analyticsCell struct {
	start time.Time
	field string
}

func cell(row repository.AnalyticsRow) analyticsCell {
	return analyticsCell{row.Start, catalog.Field(row.Metric, row.Dims...)}
}

func mergeAnalytics(live []analytics.Row, stored []repository.AnalyticsRow, q analytics.Query) []repository.AnalyticsRow {
	cells := map[analyticsCell]repository.AnalyticsRow{}
	zeroTotals(cells, q)
	for _, row := range stored {
		zeroCurrent(cells, row, q)
	}
	for _, row := range live {
		value := repository.AnalyticsRow{Window: row.Window, Start: row.Start, Metric: row.Metric, Dims: row.Dims, Value: row.Value}
		zeroCurrent(cells, value, q)
		cells[cell(value)] = value
	}
	keepClosed(cells, stored, q)
	rows := make([]repository.AnalyticsRow, 0, len(cells))
	for _, row := range cells {
		rows = append(rows, row)
	}
	slices.SortFunc(rows, compareAnalytics)
	return rows
}

func zeroTotals(cells map[analyticsCell]repository.AnalyticsRow, q analytics.Query) {
	for _, metric := range q.Metrics {
		if spec, _ := catalog.Lookup(metric); len(spec.Dims) == 0 {
			zeroCurrent(cells, repository.AnalyticsRow{Metric: metric}, q)
		}
	}
}

func keepClosed(cells map[analyticsCell]repository.AnalyticsRow, stored []repository.AnalyticsRow, q analytics.Query) {
	for _, row := range stored {
		if !catalog.PeriodEnd(q.Window, row.Start).After(q.Now) {
			cells[cell(row)] = row
		}
	}
}

func zeroCurrent(cells map[analyticsCell]repository.AnalyticsRow, row repository.AnalyticsRow, q analytics.Query) {
	spec, _ := catalog.Lookup(row.Metric)
	if !analytics.SupportsWindow(spec, q.Window) {
		return
	}
	row.Window, row.Start, row.Value = q.Window, catalog.PeriodStart(q.Window, q.Now), 0
	key := cell(row)
	if _, exists := cells[key]; !exists {
		cells[key] = row
	}
}

func compareAnalytics(a, b repository.AnalyticsRow) int {
	if order := a.Start.Compare(b.Start); order != 0 {
		return order
	}
	return cmp.Compare(catalog.Field(a.Metric, a.Dims...), catalog.Field(b.Metric, b.Dims...))
}
