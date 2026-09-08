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

// Pool statistics exist because the obvious fix for a gate wait moves the
// wait somewhere nothing was watching.
//
// The gate (pkg/db/gate.go) and the pool (openPool) are two queues in series,
// and DB_QUERY_CONCURRENCY / DB_MAX_OPEN_CONNS size them independently. Raise
// DB_QUERY_CONCURRENCY above the pool size to clear a gate wait and the
// requests do not stop queueing: they queue one layer down, in database/sql's
// own connRequest list, which emits nothing at all. gateWaitSegment would then
// go quiet and the transaction would stay exactly as slow, which reads as "the
// fix worked and something else regressed". sql.DBStats is the only view of
// that second queue, and nothing in this repo read it before.
//
// WaitCount and WaitDuration are the two that answer the question: a nonzero
// WaitCount delta means requests are waiting on a connection, and the
// WaitDuration delta is how long they waited. InUse / Idle / Open are the
// context needed to tell saturation (InUse pinned at Open, at the cap) from a
// pool that is simply cold.
const (
	// poolStatsInterval is deliberately its own constant rather than a reuse
	// of keepAliveInterval: keepalive's 2 minutes is chosen against
	// connMaxIdleTime, while this cadence is chosen against how fast an
	// operator wants to see saturation appear during an incident. One
	// Stats() call is a mutex acquire and a struct copy, so 60s is free.
	poolStatsInterval = 60 * time.Second

	poolStatsEnabledEnvVar = "DB_POOL_STATS_ENABLED"
)

// New Relic prefixes anything given to RecordCustomMetric with "Custom/", so
// these names appear in APM as Custom/db/pool/*.
const (
	metricWaitCount      = "db/pool/wait_count"
	metricWaitDurationMS = "db/pool/wait_duration_ms"
	metricInUse          = "db/pool/in_use"
	metricIdle           = "db/pool/idle"
	metricOpen           = "db/pool/open"
)

// startPoolStats launches the background sampler, unless disabled with
// DB_POOL_STATS_ENABLED=false or there is nowhere to send the numbers. A nil
// application is the no-license-key case (pkg/monitor) and every unit test:
// the loop is skipped outright rather than run into no-op agent calls, since
// with no app there is nothing to observe.
func startPoolStats(pool *sql.DB, app *newrelic.Application) {
	if app == nil {
		return
	}
	if !env.GetBool(poolStatsEnabledEnvVar, true) {
		return
	}
	go poolStatsLoop(context.Background(), pool, app)
}

// poolStatsLoop mirrors keepAliveLoop's shape (pkg/db/keepalive.go):
// fire-and-forget, background context because NewDriver takes none, and
// terminating on ctx. It differs in one way, on purpose: keepAliveLoop can
// stop itself because its pings report a closed pool, while Stats() reports no
// error and database/sql exposes no closed signal. A sampler outliving its
// pool costs one struct copy a minute and records zeros, so this loop ends
// with the process instead of inventing a probe to detect closure.
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

// recordPoolStats reports WaitCount and WaitDuration as per-interval deltas.
// They are cumulative counters over the pool's whole life, so recording them
// raw would draw a monotonically rising line that says nothing about now; the
// delta is what shows a burst. The gauges are recorded as read.
func recordPoolStats(app *newrelic.Application, current sql.DBStats, previous sql.DBStats) {
	waited := current.WaitDuration - previous.WaitDuration

	app.RecordCustomMetric(metricWaitCount, float64(current.WaitCount-previous.WaitCount))
	app.RecordCustomMetric(metricWaitDurationMS, float64(waited.Milliseconds()))
	app.RecordCustomMetric(metricInUse, float64(current.InUse))
	app.RecordCustomMetric(metricIdle, float64(current.Idle))
	app.RecordCustomMetric(metricOpen, float64(current.OpenConnections))
}
