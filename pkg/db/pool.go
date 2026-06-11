package db

import "database/sql"

func openPool(dsn string, maxConns int) (*sql.DB, error) {

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

	return pool, nil
}
