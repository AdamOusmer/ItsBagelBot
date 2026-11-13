// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/admin/repository"
	catalog "ItsBagelBot/internal/domain/analytics"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
)

const (
	analyticsDefaultDays = 30
	analyticsMaxDays     = 400
	analyticsMaxMetrics  = 32
)

func (a *adminRPC) analytics(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	window, metrics, refused := analyticsQuery(req)
	if refused.Code != "" {
		return adminError(refused)
	}
	days := req.Days
	if days <= 0 {
		days = analyticsDefaultDays
	}
	to := time.Now().UTC()
	from := catalog.PeriodStart(window, to.AddDate(0, 0, -clamp(days, 1, analyticsMaxDays)))
	rows, err := a.analyticsSeries(ctx, repository.SeriesQuery{Metrics: metrics, Window: window, From: from, To: to})
	if err != nil {
		return adminError(refusal(err))
	}
	return usersrpc.AdminReply{Analytics: &usersrpc.AdminAnalyticsView{Window: catalog.WindowCode(window), Rows: analyticsRows(rows)}}
}

func analyticsQuery(req usersrpc.AdminRequest) (catalog.Window, []catalog.Metric, domainrpc.Refusal) {
	window, ok := catalog.ParseWindow(req.Window)
	if !ok {
		window = catalog.Day
	}
	if len(req.Metrics) == 0 || len(req.Metrics) > analyticsMaxMetrics {
		return 0, nil, domainrpc.Refused(domainrpc.CodeInvalid, "between 1 and 32 metrics required")
	}
	metrics := make([]catalog.Metric, 0, len(req.Metrics))
	for _, name := range req.Metrics {
		if _, known := catalog.Lookup(catalog.Metric(name)); !known {
			return 0, nil, domainrpc.Refused(domainrpc.CodeInvalid, "unknown metric")
		}
		metrics = append(metrics, catalog.Metric(name))
	}
	return window, metrics, domainrpc.Refusal{}
}

func analyticsRows(rows []repository.AnalyticsRow) []usersrpc.AdminAnalyticsRow {
	out := make([]usersrpc.AdminAnalyticsRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, usersrpc.AdminAnalyticsRow{
			Start:  row.Start.Format(time.DateOnly),
			Metric: string(row.Metric),
			Dims:   row.Dims,
			Value:  row.Value,
		})
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
