// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"maps"
	"slices"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

// SetOnboarded must write through: the dashboard rereads the account as soon as the reply lands.
func (r *Users) SetOnboarded(ctx context.Context, id uint64, onboarded bool) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	var changed int
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		n, err := r.client.User.Update().Where(user.IDEQ(id), user.Onboarded(!onboarded)).SetOnboarded(onboarded).Save(ctx)
		changed = n
		return err
	}); err != nil {
		return err
	}
	if onboarded {
		r.analytics.Add(catalog.Lifecycle, int64(changed), "onboarded")
	}
	return r.publishChanged(ctx, id)
}

func (r *Users) persistPrefs(ctx context.Context, perUser map[uint64][]prefWrite) error {
	return db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
				for _, id := range slices.Sorted(maps.Keys(perUser)) {
					if err := writeUserPrefs(ctx, tx, id, perUser[id]); err != nil {
						return err
					}
				}
				return nil
			})
		})
	})
}
