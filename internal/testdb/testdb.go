// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package testdb

import (
	"io"
	"testing"
)

const Driver = "sqlite3"

type Name string

func MemDSN(name Name) string {
	return "file:" + string(name) + "?mode=memory&cache=shared&_fk=1"
}

func Open[C io.Closer](t *testing.T, name Name, open func(driver, dsn string) C) C {
	t.Helper()

	client := open(Driver, MemDSN(name))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
