// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strings"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	dbgate "ItsBagelBot/pkg/db"

	"github.com/go-sql-driver/mysql"
)

const (
	staffTxAttempts  = 3
	mysqlErrDeadlock = 1213
)

var errStaffRaced = errors.New("staff row created concurrently")

func retryableStaffTx(err error) bool {
	var me *mysql.MySQLError
	return errors.Is(err, errStaffRaced) || (errors.As(err, &me) && me.Number == mysqlErrDeadlock)
}

func (g staffGate) inStaffTx(ctx context.Context, fn func(context.Context, *ent.Tx) (string, error)) (string, error) {
	var errMsg string
	var err error
	for range staffTxAttempts {
		errMsg, err = dbgate.WithQuery(ctx, func(ctx context.Context) (string, error) {
			return runStaffTx(ctx, g.db, fn)
		})
		if !retryableStaffTx(err) {
			break
		}
	}
	return errMsg, err
}

func runStaffTx(ctx context.Context, db *ent.Client, fn func(context.Context, *ent.Tx) (string, error)) (string, error) {
	tx, err := db.Tx(ctx)
	if err != nil {
		return "", err
	}
	errMsg, err := fn(ctx, tx)
	if err != nil || errMsg != "" {
		_ = tx.Rollback()
		return errMsg, err
	}
	return "", tx.Commit()
}

// Ascending id order makes concurrent transactions queue instead of deadlock; SQLite has no row locks and serializes writers.
func lockStaff(ctx context.Context, tx *ent.Tx, rawIDs ...string) (map[uint64]*ent.AdminUser, error) {
	ids := make([]uint64, 0, len(rawIDs))
	for _, raw := range rawIDs {
		if id, err := parseID(raw); err == nil {
			ids = append(ids, id)
		}
	}
	query := func() *ent.AdminUserQuery {
		return tx.AdminUser.Query().Where(adminuser.IDIn(ids...)).Order(ent.Asc(adminuser.FieldID))
	}
	rows, err := query().ForUpdate().All(ctx)
	if err != nil && strings.Contains(err.Error(), "not supported in SQLite") {
		rows, err = query().All(ctx)
	}
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]*ent.AdminUser, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	return byID, nil
}

func writeStaffRow(ctx context.Context, tx *ent.Tx, existing *ent.AdminUser, row staffRow) error {
	if existing != nil {
		return tx.AdminUser.UpdateOneID(row.id).
			SetLogin(row.login).
			SetDisplayName(row.display).
			SetRole(row.role).
			SetActive(true).
			Exec(ctx)
	}
	err := tx.AdminUser.Create().
		SetID(row.id).
		SetLogin(row.login).
		SetDisplayName(row.display).
		SetRole(row.role).
		SetAddedBy(row.addedBy).
		SetActive(true).
		Exec(ctx)
	if ent.IsConstraintError(err) {
		return errStaffRaced
	}
	return err
}
