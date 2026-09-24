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

	startKeepAlive(pool)

	startPoolStats(pool, cfg.Monitor)

	return pool, nil
}

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
