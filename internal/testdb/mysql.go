// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testdb

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// MySQL needs MYSQL_TEST_DSN with CREATE DATABASE rights: each test creates and drops its own database.
func MySQL(t *testing.T, prefix string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run isolated real-MySQL tests")
	}
	cfg, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 20 * time.Second
	cfg.WriteTimeout = 20 * time.Second
	admin := openMySQL(t, cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, admin.PingContext(ctx))
	name := prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`")
	require.NoError(t, err)
	t.Cleanup(func() { dropMySQL(t, admin, name) })
	cfg.DBName = name
	raw := openMySQL(t, cfg)
	raw.SetMaxOpenConns(24)
	return raw
}

func openMySQL(t *testing.T, cfg *mysql.Config) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func dropMySQL(t *testing.T, admin *sql.DB, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "DROP DATABASE `"+name+"`"); err != nil {
		t.Errorf("drop temporary database %s: %v", name, err)
	}
}
