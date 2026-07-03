package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/user"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

// ApplyBilling applies one verified Tebex lifecycle event. Event timestamps
// make delivery order monotonic, while recurring-reference matching prevents
// a late event from an old subscription revoking a newer one.
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
		// The database commit may have succeeded while the change-event publish
		// failed. A Tebex retry of that exact event must re-announce the state.
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
			q.SetStatus(user.StatusPaid).
				SetSubscriptionSource("tebex").
				SetSubscriptionCancelPending(false).
				SetBillingEventAt(req.OccurredAt).
				SetBillingEventID(req.EventID)
			if req.ExpiresAt != nil {
				q.SetSubscriptionExpiresAt(*req.ExpiresAt)
			}
			if req.RecurringReference != "" {
				q.SetSubscriptionRef(req.RecurringReference)
			}

		case billingrpc.ActionCancelRequested:
			q.SetStatus(user.StatusPaid).
				SetSubscriptionSource("tebex").
				SetSubscriptionCancelPending(true).
				SetBillingEventAt(req.OccurredAt).
				SetBillingEventID(req.EventID)
			if req.ExpiresAt != nil {
				q.SetSubscriptionExpiresAt(*req.ExpiresAt)
			}
			if req.RecurringReference != "" {
				q.SetSubscriptionRef(req.RecurringReference)
			}

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
	// A first-time gift activation bumps the gifter's counter. Idempotent: a
	// replay of the same event returns early above, so this runs exactly once
	// per gift. Best-effort: a counter failure must never fail the already
	// applied entitlement (that would make Tebex retry and re-apply), so its
	// error is intentionally dropped.
	if req.Action == billingrpc.ActionActivate && req.GifterID != 0 && req.GifterID != req.UserID {
		_ = db.WithExec(ctx, func(ctx context.Context) error {
			return r.client.User.Update().Where(user.IDEQ(req.GifterID)).AddGiftsSent(1).Exec(ctx)
		})
	}
	if err := r.publishChanged(ctx, req.UserID); err != nil {
		return false, err
	}
	return true, nil
}

// SetAdminStatus owns operator grants. Paid grants require an expiry and are
// marked "admin" so Tebex lifecycle events can never revoke them.
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
	return r.publishChanged(ctx, id)
}

// ExpireSubscriptions is the safety net for grants whose terminal event never
// arrives. Operator grants expire exactly on time; Tebex gets a grace period
// so a briefly delayed renewal webhook cannot interrupt a paying customer.
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
		).All(ctx)
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
		if err := r.publishChanged(ctx, candidate.ID); err != nil {
			return count, err
		}
	}
	return count, nil
}
