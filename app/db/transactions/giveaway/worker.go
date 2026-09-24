// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"errors"
	"time"
)

var (
	ErrAwardNeedsReview = errors.New("giveaway award needs review")
	ErrProtectionShort  = errors.New("provider protection does not cover the complete prize interval")
)

// Start and End are absolute so a retry never adds months to a newly read date.
type WorkAward struct {
	ID, GiveawayID, UserID, RecurringReference, IntervalRule string
	Start, End                                               time.Time
	BillingRequired                                          bool
}

type ProviderState struct {
	Reference             string
	ProtectedUntil        *time.Time
	CancellationRequested bool
	Ambiguous             bool
}

type ProtectionProvider interface {
	Inspect(ctx context.Context, reference string) (ProviderState, error)
	Protect(ctx context.Context, reference string, until time.Time) (ProviderState, error)
}

type GrantPort interface {
	Prepare(ctx context.Context, award WorkAward) (string, error)
	Commit(ctx context.Context, award WorkAward, grantID string) error
}

type AwardPort interface {
	Load(ctx context.Context, awardID string) (WorkAward, error)
	Preparing(ctx context.Context, awardID string) error
	NeedsReview(ctx context.Context, awardID string, reason error) error
	Scheduled(ctx context.Context, awardID, grantID string) error
}

type UserLease interface {
	AcquireUserLease(ctx context.Context, userID string) (release func(), err error)
}

// Must never call Tebex while a database transaction is held.
type Worker struct {
	Awards            AwardPort
	Grants            GrantPort
	Provider          ProtectionProvider
	ProviderMutations bool
}

func (w *Worker) Fulfill(ctx context.Context, awardID string) error {
	award, err := w.Awards.Load(ctx, awardID)
	if err != nil {
		return err
	}
	if !validWorkAward(award) {
		return w.review(ctx, awardID, ErrAwardNeedsReview)
	}
	if leases, ok := w.Awards.(UserLease); ok {
		release, leaseErr := leases.AcquireUserLease(ctx, award.UserID)
		if leaseErr != nil {
			return leaseErr
		}
		defer release()
	}
	return w.execute(ctx, awardID, award)
}

func (w *Worker) execute(ctx context.Context, awardID string, award WorkAward) error {
	grantID, err := w.prepare(ctx, awardID, award)
	if err != nil {
		return err
	}
	if award.BillingRequired {
		if err := w.protect(ctx, awardID, award); err != nil {
			return err
		}
	}
	if err := w.Grants.Commit(ctx, award, grantID); err != nil {
		return err
	}
	return w.Awards.Scheduled(ctx, awardID, grantID)
}

func validWorkAward(award WorkAward) bool {
	return award.ID != "" && !award.Start.IsZero() && !award.End.IsZero() && award.End.After(award.Start)
}

func (w *Worker) prepare(ctx context.Context, awardID string, award WorkAward) (string, error) {
	if err := w.Awards.Preparing(ctx, awardID); err != nil {
		return "", err
	}
	if w.Grants == nil {
		return "", w.review(ctx, awardID, ErrAwardNeedsReview)
	}
	return w.Grants.Prepare(ctx, award)
}

func (w *Worker) protect(ctx context.Context, awardID string, award WorkAward) error {
	if w.Provider == nil {
		return w.review(ctx, awardID, ErrAwardNeedsReview)
	}
	state, err := w.inspect(ctx, award.RecurringReference)
	if err != nil {
		return w.review(ctx, awardID, err)
	}
	if state.ProtectedUntil != nil && !state.ProtectedUntil.Before(award.End) {
		return nil
	}
	if !w.ProviderMutations {
		return w.review(ctx, awardID, ErrAwardNeedsReview)
	}
	return w.mutateProtection(ctx, awardID, award)
}

func (w *Worker) mutateProtection(ctx context.Context, awardID string, award WorkAward) error {
	protected, err := w.Provider.Protect(ctx, award.RecurringReference, award.End)
	if err != nil {
		return w.review(ctx, awardID, err)
	}
	if err := validateProviderState(protected, award.RecurringReference); err != nil {
		return w.review(ctx, awardID, ErrAwardNeedsReview)
	}
	if protected.ProtectedUntil == nil || protected.ProtectedUntil.Before(award.End) {
		return w.review(ctx, awardID, ErrProtectionShort)
	}
	return nil
}

func (w *Worker) inspect(ctx context.Context, reference string) (ProviderState, error) {
	state, err := w.Provider.Inspect(ctx, reference)
	if err != nil {
		return ProviderState{}, err
	}
	if err := validateProviderState(state, reference); err != nil {
		return ProviderState{}, err
	}
	return state, nil
}

func validateProviderState(state ProviderState, reference string) error {
	if providerStateInvalid(state, reference) {
		return ErrAwardNeedsReview
	}
	return nil
}

func providerStateInvalid(state ProviderState, reference string) bool {
	return invalidProviderIdentity(state, reference) || state.Ambiguous || state.CancellationRequested
}

func invalidProviderIdentity(state ProviderState, reference string) bool {
	return reference == "" || state.Reference == "" || state.Reference != reference
}

func (w *Worker) review(ctx context.Context, awardID string, reason error) error {
	if err := w.Awards.NeedsReview(ctx, awardID, reason); err != nil {
		return err
	}
	return reason
}
