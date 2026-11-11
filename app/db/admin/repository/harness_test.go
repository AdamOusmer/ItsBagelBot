// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/enttest"
	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
)

func setup(t *testing.T) *repository.Admin {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	return repository.New(client)
}
