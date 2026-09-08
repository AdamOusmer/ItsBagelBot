// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package testdb opens the throwaway SQLite database the ent repository tests
// run against. The same DSN was spelled out in eleven test files across six
// services; keeping it here means a change to the connection string (adding a
// pragma, moving off shared cache) reaches every suite at once instead of
// leaving whichever file the sweep missed on the old flags.
//
// app/db/discord/repository is the deliberate exception: its concurrency tests
// need _busy_timeout and a file-backed database, and the reasons are recorded
// at that test's own definition rather than folded in here.
package testdb

import (
	"io"
	"testing"
)

// Driver is the database/sql driver name. Tests still need the
// `_ "github.com/mattn/go-sqlite3"` blank import to register it.
const Driver = "sqlite3"

// Name identifies one test's private database. It must be unique per test:
// shared-cache in-memory databases are addressed by this name, so two tests
// reusing one see each other's rows. Named rather than a plain string so it
// cannot be transposed with the driver at a call site.
type Name string

// MemDSN builds the in-memory DSN.
//
// cache=shared is what lets ent's migration connection and the repository's
// own connections see the same tables; _fk=1 turns foreign keys on, which
// SQLite leaves off by default and which the schema's cascades depend on.
func MemDSN(name Name) string {
	return "file:" + string(name) + "?mode=memory&cache=shared&_fk=1"
}

// Open opens one test's database through open and closes it when the test
// ends. open is a one-line closure per service because ent generates a
// separate enttest package (and a separate *ent.Client) for each schema, so
// there is no single opener this package could call itself.
func Open[C io.Closer](t *testing.T, name Name, open func(driver, dsn string) C) C {
	t.Helper()

	client := open(Driver, MemDSN(name))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
