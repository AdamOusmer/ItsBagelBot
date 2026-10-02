// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"

	baseent "entgo.io/ent"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newLoyaltyRepo(t *testing.T) (*loyaltyrepo.Loyalty, *ent.Client) {
	t.Helper()
	db, err := sql.Open(testdb.Driver, testdb.MemDSN("loyaltytransfer"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	repo := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	return repo, client
}

type seedRow struct {
	UserID, ViewerID uint64
	Login            string
	Points           int64
}

func seedBalance(t *testing.T, client *ent.Client, row seedRow) {
	t.Helper()
	err := client.Balance.Create().
		SetUserID(row.UserID).
		SetViewerID(row.ViewerID).
		SetViewerLogin(row.Login).
		SetPoints(row.Points).
		Exec(context.Background())
	require.NoError(t, err)
}

func watchRetentionRepo(t *testing.T, raw *sql.DB, dialect string) *loyaltyrepo.Loyalty {
	t.Helper()
	drv := entsql.OpenDB(dialect, raw)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(t.Context()))
	r := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	require.NoError(t, r.EnsureWatchSchema(t.Context()))
	t.Cleanup(func() { r.Close(context.Background()) })
	return r
}

func sqliteRetentionRepo(t *testing.T) (*loyaltyrepo.Loyalty, *sql.DB) {
	t.Helper()
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name("watchretention"+strconv.FormatInt(time.Now().UnixNano(), 10))))
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	return watchRetentionRepo(t, raw, "sqlite3"), raw
}

// MYSQL_TEST_DSN needs CREATE DATABASE rights: each test creates and drops its own database.
func mysqlRetentionRepo(t *testing.T) (*loyaltyrepo.Loyalty, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run isolated real-MySQL watchtime tests")
	}
	cfg, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 20 * time.Second
	cfg.WriteTimeout = 20 * time.Second
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, admin.PingContext(ctx))
	name := "bagel_watch_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, err := admin.ExecContext(cleanup, "DROP DATABASE `"+name+"`")
		if err != nil {
			t.Errorf("drop temporary watchtime database: %v", err)
		}
	})
	cfg.DBName = name
	raw, err := sql.Open("mysql", cfg.FormatDSN())
	require.NoError(t, err)
	raw.SetMaxOpenConns(24)
	t.Cleanup(func() { raw.Close() })
	return watchRetentionRepo(t, raw, "mysql"), raw
}

func watchAward(user, viewer uint64, instance int64) data.WatchAwardDTO {
	return data.WatchAwardDTO{UserID: user, AccountCreatedAt: instance, Generation: "1", LiveSession: "s", WindowID: "w", Entries: []data.LoyaltyEarnEntry{{ViewerID: viewer, ViewerLogin: "viewer", Points: 10, WatchSeconds: 300}}}
}
func failFirstUpdate(conflict error) baseent.Hook {
	var once sync.Once
	return func(next baseent.Mutator) baseent.Mutator {
		return baseent.MutateFunc(func(ctx context.Context, m baseent.Mutation) (baseent.Value, error) {
			err := error(nil)
			if m.Op().Is(baseent.OpUpdate) {
				once.Do(func() { err = conflict })
			}
			if err != nil {
				return nil, err
			}
			return next.Mutate(ctx, m)
		})
	}
}

func fundedLoyalty(t *testing.T, points int64) (*loyaltyrepo.Loyalty, *ent.Client, context.Context) {
	t.Helper()
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: points})
	return repo, client, context.Background()
}
