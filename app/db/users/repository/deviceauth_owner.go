// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

// lockDeviceOwner serializes grant creation against revoke-all on the owner's
// row; without it, a redemption that passed the ban check could insert after a
// revoke sweep. No user-row write here: every mutation bumps state_revision.
func lockDeviceOwner(ctx context.Context, tx *ent.Tx, userID uint64) (*ent.User, error) {
	return tx.User.Query().Where(user.IDEQ(userID), lockOwnerRow).Only(ctx)
}

func lockOwnerRow(s *sql.Selector) {
	if s.Dialect() == dialect.MySQL {
		s.ForUpdate()
	}
}

func requireDeviceOwnerInTx(ctx context.Context, tx *ent.Tx, userID uint64) error {
	owner, err := lockDeviceOwner(ctx, tx, userID)
	if ent.IsNotFound(err) {
		return ErrDeviceInvalidGrant
	}
	if err != nil {
		return err
	}
	if owner.Banned {
		return ErrDeviceInvalidGrant
	}
	return nil
}

// requireDeviceOwner keeps a storage failure distinct from a refusal: only a
// missing or banned owner may reach the client as a terminal invalid_grant.
func (r *Users) requireDeviceOwner(ctx context.Context, userID uint64) error {
	owner, err := r.FindUser(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		return ErrDeviceInvalidGrant
	}
	if err != nil {
		return err
	}
	if owner.Banned {
		return ErrDeviceInvalidGrant
	}
	return nil
}
