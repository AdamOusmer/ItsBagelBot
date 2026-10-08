// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

type fakeConnector struct{ open func() driver.Conn }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return c.open(), nil }

func (c fakeConnector) Open(string) (driver.Conn, error) { return c.open(), nil }

func (c fakeConnector) Driver() driver.Driver { return c }

type unusedConn struct{}

func (unusedConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }

func (unusedConn) Close() error { return nil }

func (unusedConn) Begin() (driver.Tx, error) { return nil, errors.New("unused") }

type counterRows struct {
	cols []string
	rows [][]driver.Value
}

func (r *counterRows) Columns() []string { return r.cols }

func (*counterRows) Close() error { return nil }

func (r *counterRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func deadlockError() error {
	return &mysql.MySQLError{Number: mysqlDeadlock, Message: "Deadlock found when trying to get lock"}
}

func bare() *Loyalty {
	return &Loyalty{earnPend: map[balKey]*earnSum{}, bumpPend: map[bumpKey]*bumpSum{}}
}

func fakeLoyalty(t *testing.T, open func() driver.Conn) *Loyalty {
	t.Helper()
	pool := sql.OpenDB(fakeConnector{open})
	t.Cleanup(func() { _ = pool.Close() })
	r := bare()
	r.sqldb, r.log, r.done = pool, zap.NewNop(), make(chan struct{})
	return r
}
