// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"database/sql"
	"math/rand/v2"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/go-sql-driver/mysql"
)

// openPool builds the pool from an instrumented connector rather than from a
// registered driver name. It takes the *mysql.Config instead of a formatted
// DSN so the connector, the New Relic segment builder and the connect log all
// read the same already-parsed target, and so the password never has to be
// serialized into a DSN string to get here.
func openPool(mc *mysql.Config, cfg Config) (*sql.DB, error) {

	maxConns := resolveMaxConns(cfg.MaxConns)

	connector, err := newInstrumentedConnector(mc)
	if err != nil {
		return nil, err
	}
	pool := sql.OpenDB(connector)

	pool.SetMaxOpenConns(maxConns)
	pool.SetMaxIdleConns(maxConns)
	pool.SetConnMaxLifetime(jitteredConnMaxLifetime())
	pool.SetConnMaxIdleTime(connMaxIdleTime)

	// SetMaxIdleConns above only *permits* the pool to hold connections open;
	// nothing establishes or refreshes them between requests. startKeepAlive
	// is what keeps a small floor genuinely warm - see keepalive.go.
	startKeepAlive(pool)

	// And startPoolStats is what makes the queue in front of those connections
	// visible, see stats.go.
	startPoolStats(pool, cfg.Monitor)

	return pool, nil
}

// jitteredConnMaxLifetime returns connMaxLifetime plus a random offset in
// [0, connMaxLifetimeJitter), drawn once per pool at open, giving an effective
// range of 30 to 40 minutes per pod.
//
// Why jitter at all: database/sql closes every connection when it hits the
// lifetime, and every pod opens its pool during the same rollout, so a fixed
// lifetime leaves those clocks phase-aligned across the whole fleet. Observed
// in New Relic on 2026-09-07 as 205 to 220ms cold connects landing in the same
// second across different services and pods, against a 1.4 to 4ms query p50.
// The full measurement is recorded on connMaxLifetimeJitter in provider.go.
//
// Why here rather than in database/sql: SetConnMaxLifetime takes one exact
// duration and the package has no jitter knob, so the only place spread can be
// introduced is the value handed to it. Per process, not per connection,
// because the pool stores a single lifetime for all of them.
//
// math/rand/v2's global source is randomly seeded per process, so this needs
// no explicit seeding and two pods started by the same rollout do not draw the
// same offset.
func jitteredConnMaxLifetime() time.Duration {
	return connMaxLifetime + rand.N(connMaxLifetimeJitter)
}

func resolveMaxConns(maxConns int) int {
	if maxConns <= 0 {
		maxConns = env.GetInt("DB_MAX_OPEN_CONNS", defaultMaxConns)
	}
	if maxConns <= 0 {
		maxConns = defaultMaxConns
	}
	return maxConns
}
