// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	dbgate "ItsBagelBot/pkg/db"
	"context"
)

type staffRow struct {
	id      uint64
	login   string
	display string
	role    adminuser.Role
	addedBy uint64
}

func upsertStaffRow(ctx context.Context, db *ent.Client, row staffRow) error {
	_, err := dbgate.WithQuery(ctx, func(ctx context.Context) (*ent.AdminUser, error) {
		return db.AdminUser.Query().Where(adminuser.IDEQ(row.id)).Only(ctx)
	})
	if err == nil {
		return updateStaffRow(ctx, db, row)
	}
	if !ent.IsNotFound(err) {
		return err
	}
	return dbgate.WithExec(ctx, func(ctx context.Context) error {
		err := db.AdminUser.Create().
			SetID(row.id).
			SetLogin(row.login).
			SetDisplayName(row.display).
			SetRole(row.role).
			SetAddedBy(row.addedBy).
			SetActive(true).
			Exec(ctx)
		if ent.IsConstraintError(err) {
			return updateStaffRow(ctx, db, row)
		}
		return err
	})
}

func updateStaffRow(ctx context.Context, db *ent.Client, row staffRow) error {
	return dbgate.WithExec(ctx, func(ctx context.Context) error {
		return db.AdminUser.UpdateOneID(row.id).
			SetLogin(row.login).
			SetDisplayName(row.display).
			SetRole(row.role).
			SetActive(true).
			Exec(ctx)
	})
}
