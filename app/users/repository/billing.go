// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	if err := r.publishChanged(ctx, req.UserID); err != nil {
		return false, err
	}
	return true, nil
}

// countGiftForGifter bumps the gifter's gifts_sent by one when this apply is a
// first-time gift activation (a gift carries a non-zero GifterID distinct from
// the recipient; self-purchases and renewals do not). Idempotent because event
// replays return early in ApplyBilling before this runs, so a gift counts once.
// Best-effort: a counter failure must never fail the already applied
// entitlement (that would make Tebex retry and re-apply), so its error is
// intentionally dropped.
func (r *Users) countGiftForGifter(ctx context.Context, req billingrpc.ApplyRequest) {
	if req.Action != billingrpc.ActionActivate || req.GifterID == 0 || req.GifterID == req.UserID {
		return
	}
	_ = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.User.Update().Where(user.IDEQ(req.GifterID)).AddGiftsSent(1).Exec(ctx)
	})
}

// applyPaidUpdate sets the paid-tier fields common to a Tebex activation and a
// cancellation-requested event; the two differ only in whether the cancellation
// is pending. One place, so the update chain is not duplicated per action.
//
// storedExpiresAt is the row's expiry before this update runs. The caller
// (transactions/web's applyBilling) already backfills a fallback expiry for
// every action that reaches this function, but that is a second place trusted
// to get it right, not a guarantee. A payment.dispute.won lands here as
// ActionCancelAborted after the preceding payment.dispute.opened already
// cleared subscription_expires_at via ActionRevoke; if req.ExpiresAt were
// ever nil for that event (a one-time purchase carries no payment-subject
// expiry), setting StatusPaid with no expiry at all would reinstate the user
// with permanent premium; see the incident this function's fallback exists
// for. This is the backstop that makes it structurally impossible from this
// side too: when the request carries no expiry and the stored row has none
// either, clamp to one month from the event instead of leaving it open. A
// hard reject was considered and rejected: SetAdminStatus can afford to
// reject because it is a synchronous, operator-driven call the caller retries
// by hand, but this path is a Tebex webhook, and ApplyBilling's contract is
// that a returned error makes Tebex retry the delivery (see its doc comment).
// That retry path exists for transient NATS/users outages, not a condition
// that Tebex redelivering the identical event can ever resolve, so rejecting
// here would leave a legitimately paid customer (a won dispute) stuck in an
// infinite retry loop instead of holding a bounded grant.
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
