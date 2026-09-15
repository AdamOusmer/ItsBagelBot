package giveaway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type workerAwards struct {
	award                        WorkAward
	preparing, scheduled, review bool
	reason                       error
}

func (a *workerAwards) Load(context.Context, string) (WorkAward, error) { return a.award, nil }
func (a *workerAwards) Preparing(context.Context, string) error         { a.preparing = true; return nil }
func (a *workerAwards) NeedsReview(_ context.Context, _ string, reason error) error {
	a.review = true
	a.reason = reason
	return nil
}
func (a *workerAwards) Scheduled(context.Context, string, string) error {
	a.scheduled = true
	return nil
}

type workerGrants struct{ prepared, committed bool }

func (g *workerGrants) Prepare(context.Context, WorkAward) (string, error) {
	g.prepared = true
	return "grant-1", nil
}
func (g *workerGrants) Commit(context.Context, WorkAward, string) error {
	g.committed = true
	return nil
}

type workerProvider struct {
	state          ProviderState
	protected      bool
	protectUntil   *time.Time
	protectError   error
	protectedState *ProviderState
}

func (p *workerProvider) Inspect(context.Context, string) (ProviderState, error) { return p.state, nil }
func (p *workerProvider) Protect(_ context.Context, ref string, until time.Time) (ProviderState, error) {
	p.protected = true
	if p.protectError != nil {
		return ProviderState{}, p.protectError
	}
	if p.protectUntil != nil {
		until = *p.protectUntil
	}
	if p.protectedState != nil {
		return *p.protectedState, nil
	}
	return ProviderState{Reference: ref, ProtectedUntil: &until}, nil
}

type failingCommitGrants struct{ workerGrants }

func (g *failingCommitGrants) Commit(context.Context, WorkAward, string) error {
	return errors.New("commit response lost")
}

func workerAward() WorkAward {
	return WorkAward{ID: "a", UserID: "1", Start: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), BillingRequired: true, RecurringReference: "r"}
}

func TestWorkerPreparesBeforeProviderAndCommitsAfterProtection(t *testing.T) {
	awards, grants, provider := &workerAwards{award: workerAward()}, &workerGrants{}, &workerProvider{state: ProviderState{Reference: "r"}}
	require.NoError(t, (&Worker{Awards: awards, Grants: grants, Provider: provider, ProviderMutations: true}).Fulfill(context.Background(), "a"))
	require.True(t, grants.prepared)
	require.True(t, provider.protected)
	require.True(t, grants.committed)
	require.True(t, awards.scheduled)
}

func TestWorkerAcceptsExistingProtectionWithoutMutationGate(t *testing.T) {
	end := workerAward().End
	awards, grants, provider := &workerAwards{award: workerAward()}, &workerGrants{}, &workerProvider{state: ProviderState{Reference: "r", ProtectedUntil: &end}}
	require.NoError(t, (&Worker{Awards: awards, Grants: grants, Provider: provider}).Fulfill(context.Background(), "a"))
	require.False(t, provider.protected)
}

func TestWorkerLeavesPreparedAwardForReviewWhenProtectionUnavailable(t *testing.T) {
	awards, grants := &workerAwards{award: workerAward()}, &workerGrants{}
	err := (&Worker{Awards: awards, Grants: grants, Provider: &workerProvider{state: ProviderState{Reference: "r"}}, ProviderMutations: false}).Fulfill(context.Background(), "a")
	require.ErrorIs(t, err, ErrAwardNeedsReview)
	require.True(t, awards.review)
	require.True(t, grants.prepared)
	require.False(t, grants.committed)
}

func TestWorkerRejectsMissingReferenceBeforeProviderMutation(t *testing.T) {
	award := workerAward()
	award.RecurringReference = ""
	awards, grants := &workerAwards{award: award}, &workerGrants{}
	err := (&Worker{Awards: awards, Grants: grants, Provider: &workerProvider{state: ProviderState{Reference: ""}}, ProviderMutations: true}).Fulfill(context.Background(), "a")
	assertReviewWithoutCommit(t, err, awards, grants)
}

func TestWorkerRejectsProtectionShortfallAfterMutation(t *testing.T) {
	short := workerAward().End.Add(-time.Hour)
	awards, grants, provider := &workerAwards{award: workerAward()}, &workerGrants{}, &workerProvider{state: ProviderState{Reference: "r"}, protectUntil: &short}
	err := (&Worker{Awards: awards, Grants: grants, Provider: provider, ProviderMutations: true}).Fulfill(context.Background(), "a")
	require.ErrorIs(t, err, ErrProtectionShort)
	assertReviewWithoutCommit(t, err, awards, grants)
}

func TestWorkerRejectsCancellationReportedAfterProtection(t *testing.T) {
	end := workerAward().End
	awards, grants, provider := &workerAwards{award: workerAward()}, &workerGrants{}, &workerProvider{
		state:          ProviderState{Reference: "r"},
		protectedState: &ProviderState{Reference: "r", ProtectedUntil: &end, CancellationRequested: true},
	}
	err := (&Worker{Awards: awards, Grants: grants, Provider: provider, ProviderMutations: true}).Fulfill(context.Background(), "a")
	assertReviewWithoutCommit(t, err, awards, grants)
}

func TestWorkerLeavesPreparedGrantWhenCommitResponseIsLost(t *testing.T) {
	end := workerAward().End
	awards, grants, provider := &workerAwards{award: workerAward()}, &failingCommitGrants{}, &workerProvider{state: ProviderState{Reference: "r"}, protectUntil: &end}
	err := (&Worker{Awards: awards, Grants: grants, Provider: provider, ProviderMutations: true}).Fulfill(context.Background(), "a")
	require.Error(t, err)
	require.True(t, grants.prepared)
	require.False(t, awards.scheduled)
}

func assertReviewWithoutCommit(t *testing.T, err error, awards *workerAwards, grants *workerGrants) {
	t.Helper()
	require.Error(t, err)
	require.True(t, awards.review)
	require.False(t, grants.committed)
}
