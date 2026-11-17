// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/premiumgrant"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

// Must never touch VIP or billing-sourced paid rows.
func (r *Users) projectAccess(ctx context.Context, id uint64, now time.Time) error {
	u, err := r.FindUser(ctx, id)
	if err != nil {
		return err
	}
	return r.applyGrantAccess(ctx, u, now)
}

func (r *Users) applyGrantAccess(ctx context.Context, u *ent.User, now time.Time) error {
	if u.Status == user.StatusVip {
		return nil
	}
	covering, err := r.hasCoveringGrant(ctx, u.ID, now)
	if err != nil {
		return err
	}
	if covering {
		return r.promoteGrantPaid(ctx, u)
	}
	return r.restoreGrantPaid(ctx, u)
}

func (r *Users) promoteGrantPaid(ctx context.Context, u *ent.User) error {
	if u.Status == user.StatusPaid {
		return nil
	}
	return r.writeAccessStatus(ctx, u, user.StatusPaid)
}

func (r *Users) restoreGrantPaid(ctx context.Context, u *ent.User) error {
	if billingOwnsAccess(u) {
		return nil
	}
	if u.Status != user.StatusPaid {
		return nil
	}
	return r.writeAccessStatus(ctx, u, user.StatusFree)
}

func billingOwnsAccess(u *ent.User) bool {
	return u.SubscriptionSource == "tebex" || u.SubscriptionSource == "admin"
}

func (r *Users) hasCoveringGrant(ctx context.Context, userID uint64, now time.Time) (bool, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.PremiumGrant.Query().Where(
			premiumgrant.UserIDEQ(userID),
			premiumgrant.StateEQ(premiumgrant.StateCommitted),
			premiumgrant.StartAtLTE(now),
			premiumgrant.EndAtGT(now),
		).Exist(ctx)
	})
}

func (r *Users) writeAccessStatus(ctx context.Context, u *ent.User, status user.Status) error {
	updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.accessStatusUpdate(u, status).Save(ctx)
	})
	if err != nil {
		return err
	}
	if updated == 0 {
		r.log.Debug("grant access projection lost concurrent ownership race", zap.Uint64("user_id", u.ID), zap.String("status", string(status)))
		return nil
	}
	r.views.Invalidate(cache.UserKey(userKeyPrefix, u.ID))
	r.stats.Invalidate(userStatsKey)
	return nil
}

func (r *Users) accessStatusUpdate(u *ent.User, status user.Status) *ent.UserUpdate {
	q := r.client.User.Update().Where(
		user.IDEQ(u.ID), user.StatusEQ(u.Status), user.StatusNEQ(user.StatusVip),
		user.SubscriptionSourceEQ(u.SubscriptionSource), user.SubscriptionSourceNotIn("tebex", "admin"),
		user.CreatedAtEQ(u.CreatedAt),
	).SetStatus(status)
	if status == user.StatusPaid && u.SubscriptionSource == "" {
		q.SetSubscriptionSource("giveaway")
	}
	if status == user.StatusFree && u.SubscriptionSource == "giveaway" {
		q.SetSubscriptionSource("")
	}
	return q
}

func (r *Users) reconcileGrantAccess(ctx context.Context, now time.Time) error {
	rows, err := r.freeUsersWithCoveringGrant(ctx, now)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := r.announceGrantAccess(ctx, row.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) freeUsersWithCoveringGrant(ctx context.Context, now time.Time) ([]*ent.User, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().Where(
			user.StatusEQ(user.StatusFree),
			user.SubscriptionSourceNotIn("tebex", "admin"),
			user.HasPremiumGrantsWith(
				premiumgrant.StateEQ(premiumgrant.StateCommitted),
				premiumgrant.StartAtLTE(now),
				premiumgrant.EndAtGT(now),
			),
		).Select(user.FieldID).All(ctx)
	})
}

func (r *Users) announceGrantAccess(ctx context.Context, userID uint64, now time.Time) error {
	if err := r.projectAccess(ctx, userID, now); err != nil {
		return err
	}
	if err := r.publishChanged(ctx, userID); err != nil {
		return err
	}
	return r.publishStatusInvalidation(ctx, userID)
}
