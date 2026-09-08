// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"database/sql"

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
	pool.SetConnMaxLifetime(connMaxLifetime)
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

func resolveMaxConns(maxConns int) int {
	if maxConns <= 0 {
		maxConns = env.GetInt("DB_MAX_OPEN_CONNS", defaultMaxConns)
	}
	if maxConns <= 0 {
		maxConns = defaultMaxConns
	}
	return maxConns
}
