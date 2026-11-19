// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type transactionDriver struct{ dialect.Driver }

func EntTransactionDriver(name string, tx *sql.Tx) dialect.Driver {
	return transactionDriver{entsql.NewDriver(name, entsql.Conn{ExecQuerier: tx})}
}

func (d transactionDriver) Tx(context.Context) (dialect.Tx, error) {
	return dialect.NopTx(d), nil
}

func (transactionDriver) Close() error { return nil }
