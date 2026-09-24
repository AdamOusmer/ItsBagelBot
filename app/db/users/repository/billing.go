// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

func (r *Users) ApplyBilling(ctx context.Context, req billingrpc.ApplyRequest) (bool, error) {
	if err := validate.UserID(req.UserID); err != nil {
		return false, err
	}
	if req.EventID == "" || req.OccurredAt.IsZero() {
		return false, errors.New("billing event id and timestamp are required")
	}

	u, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(req.UserID)).Only(ctx)
	})
	if err != nil {
		return false, err
	}
	if u.BillingEventAt != nil && req.OccurredAt.Before(*u.BillingEventAt) {
		return false, nil
	}
	if u.BillingEventAt != nil && req.OccurredAt.Equal(*u.BillingEventAt) &&
		u.BillingEventID != nil && *u.BillingEventID == req.EventID {
		return true, r.publishChanged(ctx, req.UserID)
	}
	if u.Status == user.StatusVip {
		return false, nil
	}
	if req.Action == billingrpc.ActionRevoke && u.SubscriptionSource != "tebex" {
		return false, nil
	}
	if req.Action == billingrpc.ActionRevoke && req.RecurringReference != "" &&
		u.SubscriptionRef != nil && *u.SubscriptionRef != req.RecurringReference {
		return false, nil
	}

	updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		q := r.client.User.Update().Where(
			user.IDEQ(req.UserID),
			user.Or(user.BillingEventAtIsNil(), user.BillingEventAtLTE(req.OccurredAt)),
		)
		switch req.Action {
		case billingrpc.ActionActivate, billingrpc.ActionCancelAborted:
			applyPaidUpdate(q, req, false, u.SubscriptionExpiresAt)

		case billingrpc.ActionCancelRequested:
			applyPaidUpdate(q, req, true, u.SubscriptionExpiresAt)

		case billingrpc.ActionRevoke:
			q.SetStatus(user.StatusFree).
				SetSubscriptionSource("").
				SetSubscriptionCancelPending(false).
				ClearSubscriptionExpiresAt().
				ClearSubscriptionRef().
				SetBillingEventAt(req.OccurredAt).
				SetBillingEventID(req.EventID)

		default:
			return 0, errors.New("invalid billing action")
		}
		return q.Save(ctx)
	})
	if err != nil || updated == 0 {
		return false, err
	}
	r.countGiftForGifter(ctx, req)
	if err := r.projectAccess(ctx, req.UserID, time.Now().UTC()); err != nil {
		return false, err
	}
	if err := r.publishChanged(ctx, req.UserID); err != nil {
		return false, err
	}
	return true, nil
}

// Best effort: failing here makes Tebex retry and re-apply the entitlement.
func (r *Users) countGiftForGifter(ctx context.Context, req billingrpc.ApplyRequest) {
	if req.Action != billingrpc.ActionActivate || req.GifterID == 0 || req.GifterID == req.UserID {
		return
	}
	_ = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.User.Update().Where(user.IDEQ(req.GifterID)).AddGiftsSent(1).Exec(ctx)
	})
}

// An open expiry grants permanent premium; rejecting would loop Tebex retries forever.
func applyPaidUpdate(q *ent.UserUpdate, req billingrpc.ApplyRequest, cancelPending bool, storedExpiresAt *time.Time) {
	q.SetStatus(user.StatusPaid).
		SetSubscriptionSource("tebex").
		SetSubscriptionCancelPending(cancelPending).
		SetBillingEventAt(req.OccurredAt).
		SetBillingEventID(req.EventID)
	switch {
	case req.ExpiresAt != nil:
		q.SetSubscriptionExpiresAt(*req.ExpiresAt)
	case storedExpiresAt == nil:
		q.SetSubscriptionExpiresAt(req.OccurredAt.AddDate(0, 1, 0))
	}
	if req.RecurringReference != "" {
		q.SetSubscriptionRef(req.RecurringReference)
	}
}

func (r *Users) SetAdminStatus(ctx context.Context, id uint64, status user.Status, expiresAt *time.Time) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := validate.Status(string(status)); err != nil {
		return err
	}
	if status == user.StatusPaid && (expiresAt == nil || !expiresAt.After(time.Now())) {
		return errors.New("paid status requires a future expiry")
	}

	err := db.WithExec(ctx, func(ctx context.Context) error {
		q := r.client.User.UpdateOneID(id).
			SetStatus(status).
			SetSubscriptionCancelPending(false).
			ClearSubscriptionRef().
			SetBillingEventAt(time.Now()).
			ClearBillingEventID()
		if status == user.StatusPaid {
			q.SetSubscriptionSource("admin").SetSubscriptionExpiresAt(*expiresAt)
		} else {
			q.SetSubscriptionSource("").ClearSubscriptionExpiresAt()
		}
		return q.Exec(ctx)
	})
	if err != nil {
		return err
	}
	if err := r.projectAccess(ctx, id, time.Now().UTC()); err != nil {
		return err
	}
	return r.publishChanged(ctx, id)
}

func (r *Users) ExpireSubscriptions(ctx context.Context, now time.Time, tebexGrace time.Duration) (int, error) {
	expired, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().Where(
			user.StatusEQ(user.StatusPaid),
			user.SubscriptionExpiresAtNotNil(),
			user.Or(
				user.And(
					user.SubscriptionSourceEQ("admin"),
					user.SubscriptionExpiresAtLTE(now),
				),
				user.And(
					user.SubscriptionSourceEQ("tebex"),
					user.SubscriptionExpiresAtLTE(now.Add(-tebexGrace)),
				),
			),
		).Select(user.FieldSubscriptionSource).All(ctx)
	})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, candidate := range expired {
		cutoff := now
		if candidate.SubscriptionSource == "tebex" {
			cutoff = now.Add(-tebexGrace)
		}
		updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
			return r.client.User.Update().Where(
				user.IDEQ(candidate.ID),
				user.StatusEQ(user.StatusPaid),
				user.SubscriptionSourceEQ(candidate.SubscriptionSource),
				user.SubscriptionExpiresAtLTE(cutoff),
			).
				SetStatus(user.StatusFree).
				SetSubscriptionSource("").
				SetSubscriptionCancelPending(false).
				ClearSubscriptionExpiresAt().
				ClearSubscriptionRef().
				Save(ctx)
		})
		if err != nil {
			return count, err
		}
		if updated == 0 {
			continue
		}
		count++
		if err := r.projectAccess(ctx, candidate.ID, now); err != nil {
			return count, err
		}
		if err := r.publishChanged(ctx, candidate.ID); err != nil {
			return count, err
		}
	}
	return count, nil
}
