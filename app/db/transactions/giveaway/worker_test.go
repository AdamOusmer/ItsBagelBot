// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type workerAwards struct {
	award                        WorkAward
	preparing, scheduled, review bool
}

func (a *workerAwards) Load(context.Context, string) (WorkAward, error) { return a.award, nil }
func (a *workerAwards) Preparing(context.Context, string) error         { a.preparing = true; return nil }
func (a *workerAwards) NeedsReview(context.Context, string, error) error {
	a.review = true
	return nil
}
func (a *workerAwards) Scheduled(context.Context, string, string) error {
	a.scheduled = true
	return nil
}

type workerGrants struct {
	prepared, committed bool
	commitErr           error
}

func (g *workerGrants) Prepare(context.Context, WorkAward) (string, error) {
	g.prepared = true
	return "grant-1", nil
}

func (g *workerGrants) Commit(context.Context, WorkAward, string) error {
	g.committed = g.commitErr == nil
	return g.commitErr
}

type workerProvider struct {
	state          ProviderState
	protected      bool
	protectUntil   *time.Time
	protectedState *ProviderState
}

func (p *workerProvider) Inspect(context.Context, string) (ProviderState, error) { return p.state, nil }
func (p *workerProvider) Protect(_ context.Context, ref string, until time.Time) (ProviderState, error) {
	p.protected = true
	if p.protectedState != nil {
		return *p.protectedState, nil
	}
	if p.protectUntil != nil {
		until = *p.protectUntil
	}
	return ProviderState{Reference: ref, ProtectedUntil: &until}, nil
}

type workerOutcome struct{ prepared, protected, committed, scheduled, review bool }

func workerAward() WorkAward {
	return WorkAward{ID: "a", UserID: "1", Start: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), BillingRequired: true, RecurringReference: "r"}
}

func TestWorkerFulfill(t *testing.T) {
	end := workerAward().End
	short := end.Add(-time.Hour)
	commitLost := errors.New("commit response lost")
	noReference := workerAward()
	noReference.RecurringReference = ""
	for _, tc := range []struct {
		name       string
		award      WorkAward
		provider   workerProvider
		mutations  bool
		commitErr  error
		wantErr    error
		wantResult workerOutcome
	}{
		{
			name: "TestWorkerPreparesBeforeProviderAndCommitsAfterProtection", award: workerAward(),
			provider: workerProvider{state: ProviderState{Reference: "r"}}, mutations: true,
			wantResult: workerOutcome{prepared: true, protected: true, committed: true, scheduled: true},
		},
		{
			name: "TestWorkerAcceptsExistingProtectionWithoutMutationGate", award: workerAward(),
			provider:   workerProvider{state: ProviderState{Reference: "r", ProtectedUntil: &end}},
			wantResult: workerOutcome{prepared: true, committed: true, scheduled: true},
		},
		{
			name: "TestWorkerLeavesPreparedAwardForReviewWhenProtectionUnavailable", award: workerAward(),
			provider: workerProvider{state: ProviderState{Reference: "r"}},
			wantErr:  ErrAwardNeedsReview, wantResult: workerOutcome{prepared: true, review: true},
		},
		{
			name: "TestWorkerRejectsMissingReferenceBeforeProviderMutation", award: noReference,
			provider: workerProvider{state: ProviderState{}}, mutations: true,
			wantErr: ErrAwardNeedsReview, wantResult: workerOutcome{prepared: true, review: true},
		},
		{
			name: "TestWorkerRejectsProtectionShortfallAfterMutation", award: workerAward(),
			provider: workerProvider{state: ProviderState{Reference: "r"}, protectUntil: &short}, mutations: true,
			wantErr: ErrProtectionShort, wantResult: workerOutcome{prepared: true, protected: true, review: true},
		},
		{
			name: "TestWorkerRejectsCancellationReportedAfterProtection", award: workerAward(),
			provider: workerProvider{state: ProviderState{Reference: "r"}, protectedState: &ProviderState{Reference: "r", ProtectedUntil: &end, CancellationRequested: true}}, mutations: true,
			wantErr: ErrAwardNeedsReview, wantResult: workerOutcome{prepared: true, protected: true, review: true},
		},
		{
			name: "TestWorkerLeavesPreparedGrantWhenCommitResponseIsLost", award: workerAward(),
			provider: workerProvider{state: ProviderState{Reference: "r"}, protectUntil: &end}, mutations: true, commitErr: commitLost,
			wantErr: commitLost, wantResult: workerOutcome{prepared: true, protected: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			awards, grants, provider := &workerAwards{award: tc.award}, &workerGrants{commitErr: tc.commitErr}, tc.provider
			err := (&Worker{Awards: awards, Grants: grants, Provider: &provider, ProviderMutations: tc.mutations}).Fulfill(t.Context(), "a")

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantResult, workerOutcome{grants.prepared, provider.protected, grants.committed, awards.scheduled, awards.review})
		})
	}
}
