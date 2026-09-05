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

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
)

// newStore opens one test's own in-memory SQLite database and wraps it in the
// repository under test.
//
// name must be unique per test: shared-cache memory databases are addressed by
// that name, so two tests reusing one see each other's rows. _busy_timeout
// keeps the concurrency tests honest -- SQLite serializes writers with a
// database-wide lock and returns SQLITE_BUSY immediately without it, which
// would make a correctly-serialized race look like a failure.
func newStore(t *testing.T, name string) (*repository.Store, context.Context) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:discord"+name+"?mode=memory&cache=shared&_fk=1&_busy_timeout=5000")
	t.Cleanup(func() { _ = client.Close() })
	return repository.New(client, dialect.SQLite), context.Background()
}

// newConcurrentStore opens a file-backed SQLite database for the tests that
// exercise concurrent writers.
//
// The shared-cache in-memory DSN the other tests use cannot serve them: two
// connections in one shared cache hit SQLITE_LOCKED ("database table is
// locked"), which _busy_timeout does not retry -- that timeout only covers
// SQLITE_BUSY at the file lock. A file database gives each connection its own
// cache, so contention lands on the file lock where _busy_timeout applies, and
// _txlock=immediate makes every transaction take its write lock at BEGIN
// instead of upgrading mid-transaction, which is the deadlock the default
// deferred mode produces between two read-modify-write callers.
func newConcurrentStore(t *testing.T, name string) (*repository.Store, context.Context) {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), name+".db") + "?_fk=1&_busy_timeout=5000&_txlock=immediate"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })
	return repository.New(client, dialect.SQLite), context.Background()
}
