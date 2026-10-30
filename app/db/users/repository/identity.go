// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strings"

	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

type identityChange struct {
	prior   string
	changed bool
}

// SyncIdentity applies a Twitch rename to an existing account and returns the replaced login ("" when unchanged).
// Unlike Register it never creates a row: a public lookup must not sign anyone up.
func (r *Users) SyncIdentity(ctx context.Context, id uint64, username, rawDisplayName string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if err := validateIdentity(id, username); err != nil {
		return "", err
	}
	change, err := r.applyIdentity(ctx, id, username, displayName(rawDisplayName).storable())
	if err != nil || !change.changed {
		return "", err
	}
	return change.prior, r.publishChanged(ctx, id)
}

func validateIdentity(id uint64, username string) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	return validate.Username(username)
}

func (r *Users) applyIdentity(ctx context.Context, id uint64, username string, name displayName) (identityChange, error) {
	var change identityChange
	err := db.WithExec(ctx, func(ctx context.Context) error {
		existing, err := r.client.User.Query().
			Where(user.IDEQ(id)).
			Only(ctx)
		if err != nil || !name.changes(existing, username) {
			return err
		}
		if existing.Username != username {
			change.prior = existing.Username
		}
		_, err = name.apply(existing.Update().SetUsername(username)).Save(ctx)
		change.changed = err == nil
		return err
	})
	return change, err
}
