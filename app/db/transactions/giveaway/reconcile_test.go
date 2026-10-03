// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/tebex"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reconciliationSeed struct {
	id, state, ref string
	userID         uint64
}

func (f *fixture) seedReconciliation(seed reconciliationSeed) {
	f.t.Helper()
	end := f.now.Add(2 * time.Hour)
	_, err := f.newAward(seed.id).SetUserID(seed.userID).SetIntervalRule("verified").SetState(seed.state).SetBillingState("protected").SetPlannedStart(f.now).SetPlannedEnd(end).Save(f.t.Context())
	require.NoError(f.t, err)
	_, err = f.db.BillingOperation.Create().SetID("billing:" + seed.id).SetAwardID(seed.id).SetAgreementID(fmt.Sprintf("agreement:%d:%s", seed.userID, seed.ref)).SetRecurringReference(seed.ref).SetRequestedStart(f.now).SetRequestedEnd(end).SetUpdatedAt(f.now.Add(-time.Hour)).Save(f.t.Context())
	require.NoError(f.t, err)
	f.config = Config{ReconcileInterval: 15 * time.Minute, BoundaryInterval: 5 * time.Millisecond}
}

func (f *fixture) operationState(awardID string) string {
	operation, err := f.db.BillingOperation.Get(f.t.Context(), "billing:"+awardID)
	if err != nil {
		return ""
	}
	return operation.State
}

func TestProviderReconciliationPersistsAgreementAndClearsLease(t *testing.T) {
	f := newFixture(t)
	f.seedReconciliation(reconciliationSeed{id: "award-reconcile", userID: 7, state: "scheduled", ref: "ref-1"})
	paused, next := f.now.Add(24*time.Hour), f.now.Add(30*time.Minute)
	f.provider = &fakeProvider{payment: tebex.RecurringPayment{Reference: "ref-1", Status: "Paused", Interval: "P1M", NextPaymentDate: &next, PausedUntil: &paused}}

	f.runUntil(func() bool { return f.operationState("award-reconcile") == "verified" })

	assert.Equal(t, 1, f.provider.calls)
	agreement, err := f.db.TebexAgreement.Get(t.Context(), "agreement:7:ref-1")
	require.NoError(t, err)
	assert.Equal(t, "Paused", agreement.ProviderStatus)
	assert.False(t, agreement.PausedUntil.IsZero())
	assert.False(t, agreement.VerifiedAt.IsZero())
	operation, err := f.db.BillingOperation.Get(t.Context(), "billing:award-reconcile")
	require.NoError(t, err)
	assert.True(t, operation.LeaseUntil.IsZero())
}

func TestProviderReconciliationAlertsOnCancellationWithoutRevokingAward(t *testing.T) {
	f := newFixture(t)
	f.seedReconciliation(reconciliationSeed{id: "award-drift", userID: 8, state: "active", ref: "ref-2"})
	f.provider = &fakeProvider{payment: tebex.RecurringPayment{Reference: "ref-2", Status: "Paused", Interval: "P1M", CancellationRequested: true}}

	f.runUntil(func() bool { return f.operationState("award-drift") == "needs_review" })

	assert.Equal(t, "active", f.db.GiveawayAward.GetX(t.Context(), "award-drift").State)
	alerted, err := f.db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ("award-drift"), giveawayalert.CategoryEQ("billing-reconciliation")).Exist(t.Context())
	require.NoError(t, err)
	assert.True(t, alerted)
}

func TestProviderReconciliationStopsBeforeProbeWhenCancellationIsPending(t *testing.T) {
	f := newFixture(t)
	f.seedReconciliation(reconciliationSeed{id: "award-cancel-pending", userID: 9, state: "active", ref: "ref-cancel-pending"})
	ref := "ref-cancel-pending"
	f.users = &fakeUsers{coverage: usersrpc.PremiumCoverage{RecurringReference: &ref, CancelPending: true}}
	f.provider = &fakeProvider{payment: tebex.RecurringPayment{Reference: ref, Status: "Paused"}}

	f.runUntil(func() bool {
		return f.db.GiveawayAward.GetX(t.Context(), "award-cancel-pending").BillingState == "uncertain"
	})

	assert.Zero(t, f.provider.calls)
}
