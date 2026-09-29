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

type billingOutcome int

const (
	billingProceed billingOutcome = iota
	billingSkip
	billingReplay
)

func (r *Users) ApplyBilling(ctx context.Context, req billingrpc.ApplyRequest) (bool, error) {
	if err := validateApply(req); err != nil {
		return false, err
	}
	u, err := r.loadBillingUser(ctx, req.UserID)
	if err != nil {
		return false, err
	}
	switch billingOutcomeFor(u, req) {
	case billingSkip:
		return false, nil
	case billingReplay:
		return true, r.publishChanged(ctx, req.UserID)
	}
	updated, err := r.saveBilling(ctx, u, req)
	if err != nil || updated == 0 {
		return false, err
	}
	return true, r.finishBilling(ctx, req)
}

func validateApply(req billingrpc.ApplyRequest) error {
	if err := validate.UserID(req.UserID); err != nil {
		return err
	}
	if req.EventID == "" || req.OccurredAt.IsZero() {
		return errors.New("billing event id and timestamp are required")
	}
	return nil
}

func (r *Users) loadBillingUser(ctx context.Context, id uint64) (*ent.User, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(id)).Only(ctx)
	})
}

func billingOutcomeFor(u *ent.User, req billingrpc.ApplyRequest) billingOutcome {
	switch {
	case isStaleBilling(u, req):
		return billingSkip
	case isReplayedBilling(u, req):
		return billingReplay
	case u.Status == user.StatusVip || ignoresTebexScoped(u, req):
		return billingSkip
	}
	return billingProceed
}

func isStaleBilling(u *ent.User, req billingrpc.ApplyRequest) bool {
	return u.BillingEventAt != nil && req.OccurredAt.Before(*u.BillingEventAt)
}

func isReplayedBilling(u *ent.User, req billingrpc.ApplyRequest) bool {
	if u.BillingEventAt == nil || !req.OccurredAt.Equal(*u.BillingEventAt) {
		return false
	}
	return u.BillingEventID != nil && *u.BillingEventID == req.EventID
}

func (r *Users) saveBilling(ctx context.Context, u *ent.User, req billingrpc.ApplyRequest) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		q := r.client.User.Update().Where(
			user.IDEQ(req.UserID),
			user.Or(user.BillingEventAtIsNil(), user.BillingEventAtLTE(req.OccurredAt)),
		)
		if err := applyBillingAction(q, u, req); err != nil {
			return 0, err
		}
		return q.Save(ctx)
	})
}

func (r *Users) finishBilling(ctx context.Context, req billingrpc.ApplyRequest) error {
	r.countGiftForGifter(ctx, req)
	if err := r.projectAccess(ctx, req.UserID, time.Now().UTC()); err != nil {
		return err
	}
	return r.publishChanged(ctx, req.UserID)
}

type billingHandler func(q *ent.UserUpdate, u *ent.User, req billingrpc.ApplyRequest)

var billingHandlers = map[billingrpc.Action]billingHandler{
	billingrpc.ActionActivate: func(q *ent.UserUpdate, u *ent.User, req billingrpc.ApplyRequest) {
		applyPaidUpdate(q, req, false, u.SubscriptionExpiresAt)
		q.SetSubscriptionPaymentFailed(false)
	},
	billingrpc.ActionCancelAborted: func(q *ent.UserUpdate, u *ent.User, req billingrpc.ApplyRequest) {
		applyPaidUpdate(q, req, false, u.SubscriptionExpiresAt)
	},
	billingrpc.ActionCancelRequested: func(q *ent.UserUpdate, u *ent.User, req billingrpc.ApplyRequest) {
		applyPaidUpdate(q, req, true, u.SubscriptionExpiresAt)
	},
	billingrpc.ActionPaymentFailed: func(q *ent.UserUpdate, _ *ent.User, req billingrpc.ApplyRequest) {
		q.SetSubscriptionPaymentFailed(true).
			SetBillingEventAt(req.OccurredAt).
			SetBillingEventID(req.EventID)
	},
	billingrpc.ActionRevoke: func(q *ent.UserUpdate, _ *ent.User, req billingrpc.ApplyRequest) {
		clearSubscription(q)
		q.SetBillingEventAt(req.OccurredAt).SetBillingEventID(req.EventID)
	},
}

func clearSubscription(q *ent.UserUpdate) {
	q.SetStatus(user.StatusFree).
		SetSubscriptionSource("").
		SetSubscriptionCancelPending(false).
		SetSubscriptionPaymentFailed(false).
		ClearSubscriptionExpiresAt().
		ClearSubscriptionRef()
}

func applyBillingAction(q *ent.UserUpdate, u *ent.User, req billingrpc.ApplyRequest) error {
	handler, ok := billingHandlers[req.Action]
	if !ok {
		return errors.New("invalid billing action")
	}
	handler(q, u, req)
	return nil
}

func tebexScoped(action billingrpc.Action) bool {
	return action == billingrpc.ActionRevoke || action == billingrpc.ActionPaymentFailed
}

func ignoresTebexScoped(u *ent.User, req billingrpc.ApplyRequest) bool {
	if !tebexScoped(req.Action) {
		return false
	}
	return u.SubscriptionSource != "tebex" || refMismatch(u, req)
}

func refMismatch(u *ent.User, req billingrpc.ApplyRequest) bool {
	if req.RecurringReference == "" || u.SubscriptionRef == nil {
		return false
	}
	return *u.SubscriptionRef != req.RecurringReference
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

func validateAdminStatus(status user.Status, expiresAt *time.Time) error {
	if err := validate.Status(string(status)); err != nil {
		return err
	}
	if status == user.StatusPaid && !isFuture(expiresAt) {
		return errors.New("paid status requires a future expiry")
	}
	return nil
}

func isFuture(t *time.Time) bool {
	return t != nil && t.After(time.Now())
}

func (r *Users) SetAdminStatus(ctx context.Context, id uint64, status user.Status, expiresAt *time.Time) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := validateAdminStatus(status, expiresAt); err != nil {
		return err
	}
	err := db.WithExec(ctx, func(ctx context.Context) error {
		q := r.client.User.UpdateOneID(id).
			SetStatus(status).
			SetSubscriptionCancelPending(false).
			SetSubscriptionPaymentFailed(false).
			ClearSubscriptionRef().
			SetBillingEventAt(time.Now()).
			ClearBillingEventID()
		applyAdminSource(q, status, expiresAt)
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

func applyAdminSource(q *ent.UserUpdateOne, status user.Status, expiresAt *time.Time) {
	if status == user.StatusPaid {
		q.SetSubscriptionSource("admin").SetSubscriptionExpiresAt(*expiresAt)
		return
	}
	q.SetSubscriptionSource("").ClearSubscriptionExpiresAt()
}

func (r *Users) expiredCandidates(ctx context.Context, now time.Time, tebexGrace time.Duration) ([]*ent.User, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
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
}

func (r *Users) expireOne(ctx context.Context, candidate *ent.User, now time.Time, tebexGrace time.Duration) (bool, error) {
	cutoff := now
	if candidate.SubscriptionSource == "tebex" {
		cutoff = now.Add(-tebexGrace)
	}
	updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		q := r.client.User.Update().Where(
			user.IDEQ(candidate.ID),
			user.StatusEQ(user.StatusPaid),
			user.SubscriptionSourceEQ(candidate.SubscriptionSource),
			user.SubscriptionExpiresAtLTE(cutoff),
		)
		clearSubscription(q)
		return q.Save(ctx)
	})
	if err != nil || updated == 0 {
		return false, err
	}
	if err := r.projectAccess(ctx, candidate.ID, now); err != nil {
		return true, err
	}
	return true, r.publishChanged(ctx, candidate.ID)
}

func (r *Users) ExpireSubscriptions(ctx context.Context, now time.Time, tebexGrace time.Duration) (int, error) {
	expired, err := r.expiredCandidates(ctx, now, tebexGrace)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, candidate := range expired {
		done, err := r.expireOne(ctx, candidate, now, tebexGrace)
		if done {
			count++
		}
		if err != nil {
			return count, err
		}
	}
	return count, nil
}
