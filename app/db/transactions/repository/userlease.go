// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveawayuserlease"
	"ItsBagelBot/pkg/db"
)

const userLeaseReleaseTimeout = 2 * time.Second

type UserLease struct {
	TTL  time.Duration
	Busy error
	Now  func() time.Time
}

func (l UserLease) Acquire(ctx context.Context, client *ent.Client, userID uint64) (func(), error) {
	owner := uuid.NewString()
	err := db.WithTx(ctx, client, func(tx *ent.Tx) error {
		return l.claim(ctx, tx, userID, owner)
	})
	if err != nil {
		return nil, err
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), userLeaseReleaseTimeout)
		defer cancel()
		now := l.Now().UTC()
		_, _ = client.GiveawayUserLease.UpdateOneID(userLeaseID(userID)).Where(giveawayuserlease.OwnerEQ(owner)).SetLeaseUntil(now).SetUpdatedAt(now).Save(ctx)
	}, nil
}

func userLeaseID(userID uint64) string {
	return "user:" + strconv.FormatUint(userID, 10)
}

func (l UserLease) claim(ctx context.Context, tx *ent.Tx, userID uint64, owner string) error {
	now := l.Now().UTC()
	until := now.Add(l.TTL)
	row, err := lockedUserLease(ctx, tx, userID)
	if ent.IsNotFound(err) {
		_, err = tx.GiveawayUserLease.Create().SetID(userLeaseID(userID)).SetUserID(userID).SetOwner(owner).SetLeaseUntil(until).SetUpdatedAt(now).Save(ctx)
		return err
	}
	if err != nil {
		return err
	}
	if row.LeaseUntil.After(now) {
		return l.Busy
	}
	_, err = row.Update().SetOwner(owner).SetLeaseUntil(until).SetUpdatedAt(now).Save(ctx)
	return err
}

func lockedUserLease(ctx context.Context, tx *ent.Tx, userID uint64) (*ent.GiveawayUserLease, error) {
	query := tx.GiveawayUserLease.Query().Where(giveawayuserlease.UserIDEQ(userID))
	row, err := query.Clone().ForUpdate().Only(ctx)
	if db.RowLocksUnsupported(err) {
		return query.Only(ctx)
	}
	return row, err
}
