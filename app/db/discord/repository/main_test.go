// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect"

	"ItsBagelBot/app/db/discord/ent/enttest"
	"ItsBagelBot/app/db/discord/repository"

	_ "github.com/mattn/go-sqlite3"
)

func newStore(t *testing.T, name string) (*repository.Store, context.Context) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:discord"+name+"?mode=memory&cache=shared&_fk=1&_busy_timeout=5000")
	t.Cleanup(func() { _ = client.Close() })
	return repository.New(client, dialect.SQLite), context.Background()
}

func newConcurrentStore(t *testing.T, name string) (*repository.Store, context.Context) {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), name+".db") + "?_fk=1&_busy_timeout=5000&_txlock=immediate"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return repository.New(client, dialect.SQLite), context.Background()
}
