// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"database/sql"

	"ItsBagelBot/pkg/env"
)

func openPool(dsn string, maxConns int) (*sql.DB, error) {

	if maxConns <= 0 {
		maxConns = env.GetInt("DB_MAX_OPEN_CONNS", defaultMaxConns)
	}
	if maxConns <= 0 {
		maxConns = defaultMaxConns
	}

	pool, err := sql.Open("nrmysql", dsn)
	if err != nil {
		return nil, err
	}

	pool.SetMaxOpenConns(maxConns)
	pool.SetMaxIdleConns(maxConns)
	pool.SetConnMaxLifetime(connMaxLifetime)
	pool.SetConnMaxIdleTime(connMaxIdleTime)

	// SetMaxIdleConns above only *permits* the pool to hold connections open;
	// nothing establishes or refreshes them between requests. startKeepAlive
	// is what keeps a small floor genuinely warm - see keepalive.go.
	startKeepAlive(pool)

	return pool, nil
}
