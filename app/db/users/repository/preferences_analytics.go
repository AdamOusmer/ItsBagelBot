// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
)

func (r *Users) persistPrefs(ctx context.Context, perUser map[uint64][]prefWrite) error {
	onboarded, err := db.WithTxResult(ctx, r.client, func(tx *ent.Tx) (int, error) {
		return writeAllPrefs(ctx, tx, perUser)
	})
	if err != nil {
		return err
	}

	r.analytics.Add(catalog.Lifecycle, int64(onboarded), "onboarded")
	return nil
}

func writeAllPrefs(ctx context.Context, tx *ent.Tx, perUser map[uint64][]prefWrite) (int, error) {
	var onboarded int
	for id, writes := range perUser {
		n, err := persistOnboarding(ctx, tx, id, writes)
		if err != nil {
			return 0, err
		}
		if err := writeUserPrefs(ctx, tx, id, writes); err != nil {
			return 0, err
		}
		onboarded += n
	}
	return onboarded, nil
}

func persistOnboarding(ctx context.Context, tx *ent.Tx, id uint64, writes []prefWrite) (int, error) {
	var onboarded bool
	for _, w := range writes {
		if w.field == prefOnboarded {
			onboarded = w.flag
		}
	}
	if !onboarded {
		return 0, nil
	}
	return tx.User.Update().Where(user.IDEQ(id), user.Onboarded(false)).SetOnboarded(true).Save(ctx)
}
