// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/newrelic/go-agent/v3/newrelic"
)

const (
	poolStatsInterval = 60 * time.Second

	poolStatsEnabledEnvVar = "DB_POOL_STATS_ENABLED"
)

const (
	metricWaitCount      = "db/pool/wait_count"
	metricWaitDurationMS = "db/pool/wait_duration_ms"
	metricInUse          = "db/pool/in_use"
	metricIdle           = "db/pool/idle"
	metricOpen           = "db/pool/open"
)

func startPoolStats(pool *sql.DB, app *newrelic.Application) {
	if app == nil {
		return
	}
	if !env.GetBool(poolStatsEnabledEnvVar, true) {
		return
	}
	go poolStatsLoop(context.Background(), pool, app)
}

func poolStatsLoop(ctx context.Context, pool *sql.DB, app *newrelic.Application) {
	ticker := time.NewTicker(poolStatsInterval)
	defer ticker.Stop()

	previous := pool.Stats()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := pool.Stats()
			recordPoolStats(app, current, previous)
			previous = current
		}
	}
}

func recordPoolStats(app *newrelic.Application, current sql.DBStats, previous sql.DBStats) {
	waited := current.WaitDuration - previous.WaitDuration

	app.RecordCustomMetric(metricWaitCount, float64(current.WaitCount-previous.WaitCount))
	app.RecordCustomMetric(metricWaitDurationMS, float64(waited.Milliseconds()))
	app.RecordCustomMetric(metricInUse, float64(current.InUse))
	app.RecordCustomMetric(metricIdle, float64(current.Idle))
	app.RecordCustomMetric(metricOpen, float64(current.OpenConnections))
}
