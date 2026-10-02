// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/billingoperation"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/ent/giveawayfulfillmentplan"
	"ItsBagelBot/app/db/transactions/tebex"
	users "ItsBagelBot/internal/domain/rpc/users"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineRestartDoesNotDuplicatePreparedGrant(t *testing.T) {
	f := newFixture(t)
	f.users, f.config = &fakeUsers{}, Config{PromotionalGrantsEnabled: true}
	award := f.award("award-1")
	f.queue(award, "award.fulfill")

	require.NoError(t, f.engine().DispatchOnce(t.Context()))
	require.Error(t, f.engine().DispatchOnce(t.Context()))

	ledger := f.users.ledger()
	assert.Equal(t, 1, ledger.prepares)
	assert.Equal(t, 1, ledger.commits)
	assert.Equal(t, PromotionalCalendarMonthRule, ledger.prepared.IntervalRuleVersion)
	assert.Equal(t, f.now, ledger.prepared.StartAt)
	assert.Equal(t, f.now.AddDate(0, 1, 0), ledger.prepared.EndAt)
	assert.Equal(t, "active", f.db.GiveawayAward.GetX(t.Context(), award.ID).State)
	plan, err := f.db.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(t.Context())
	require.NoError(t, err)
	assert.Equal(t, PromotionalCalendarMonthRule, plan.IntervalRule)
	assert.Equal(t, f.now, plan.StartAt)
	assert.Equal(t, f.now.AddDate(0, 1, 0), plan.EndAt)
}

func TestEngineCommitRetryReusesImmutablePlanAndResolvesFulfillmentAlert(t *testing.T) {
	f := newFixture(t)
	commitErr := errors.New("temporary users commit failure")
	f.users, f.config = &fakeUsers{commitErr: commitErr}, Config{PromotionalGrantsEnabled: true}
	award := f.award("award-plan-retry")
	_, err := f.db.GiveawayAlert.Create().SetID(award.ID + ":fulfillment").SetAwardID(award.ID).SetCategory("fulfillment").SetState("unresolved").SetMessage("old commit failure").Save(t.Context())
	require.NoError(t, err)

	require.ErrorIs(t, f.dispatch(award, "award.fulfill"), commitErr)
	firstPlan, err := f.db.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(t.Context())
	require.NoError(t, err)
	firstAward := f.db.GiveawayAward.GetX(t.Context(), award.ID)

	f.users.mu.Lock()
	f.users.commitErr = nil
	f.users.mu.Unlock()
	require.NoError(t, f.dispatch(award, "award.fulfill"))

	secondPlan, err := f.db.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(t.Context())
	require.NoError(t, err)
	assert.Equal(t, firstPlan.IntervalRule, secondPlan.IntervalRule)
	assert.Equal(t, firstPlan.StartAt, secondPlan.StartAt)
	assert.Equal(t, firstPlan.EndAt, secondPlan.EndAt)
	updated := f.db.GiveawayAward.GetX(t.Context(), award.ID)
	assert.Equal(t, "provider-monthly-unverified", updated.IntervalRule)
	assert.Equal(t, "active", updated.State)
	assert.Empty(t, updated.FailureReason)
	assert.Equal(t, firstAward.PlannedStart, updated.PlannedStart)
	assert.Equal(t, firstAward.PlannedEnd, updated.PlannedEnd)
	prepared := f.users.ledger().prepared
	assert.Equal(t, PromotionalCalendarMonthRule, prepared.IntervalRuleVersion)
	assert.Equal(t, firstPlan.StartAt, prepared.StartAt)
	assert.Equal(t, firstPlan.EndAt, prepared.EndAt)
	alert, err := f.db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(award.ID), giveawayalert.CategoryEQ("fulfillment")).Only(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "resolved", alert.State)
}

func TestEngineKeepsRecurringWinnerPendingUntilProviderRuleVerified(t *testing.T) {
	f := newFixture(t)
	ref := "recurring-ref"
	f.users, f.config = &fakeUsers{coverage: users.PremiumCoverage{RecurringReference: &ref}}, Config{PromotionalGrantsEnabled: true}
	award := f.award("award-provider-gate")

	require.ErrorIs(t, f.dispatch(award, "award.fulfill"), ErrAwardNeedsReview)

	assert.Equal(t, "needs_review", f.db.GiveawayAward.GetX(t.Context(), award.ID).State)
	assert.Zero(t, f.users.ledger().prepares)
}

func TestEngineDoesNotBypassDisabledRuleForSavedPlan(t *testing.T) {
	f := newFixture(t)
	award, err := f.newAward("award-saved-plan-gate").SetIntervalRule(PromotionalCalendarMonthRule).SetPlannedStart(f.now).SetPlannedEnd(f.now.AddDate(0, 1, 0)).Save(t.Context())
	require.NoError(t, err)

	_, err = f.engine().planAward(t.Context(), award, users.PremiumCoverage{})

	require.ErrorIs(t, err, ErrAwardNeedsReview)
	assert.Equal(t, "needs_review", f.db.GiveawayAward.GetX(t.Context(), award.ID).State)
}

func TestEngineSendsUnprotectedSubscriberToReviewWithoutCommitting(t *testing.T) {
	next := fixtureNow.Add(14 * 24 * time.Hour)
	pausedPastPrizeEnd := fixtureNow.AddDate(0, 3, 0)
	for _, tc := range []struct {
		name    string
		payment tebex.RecurringPayment
	}{
		{"leaves an unpaused subscription for review", tebex.RecurringPayment{Reference: "ref-1", Status: "Active", Interval: "P1M", NextPaymentDate: &next}},
		{"does not trust a pause this engine never wrote", tebex.RecurringPayment{Reference: "ref-1", Status: "Paused", Interval: "P1M", NextPaymentDate: &next, PausedUntil: &pausedPastPrizeEnd}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ref := "ref-1"
			f.users = &fakeUsers{coverage: users.PremiumCoverage{RecurringReference: &ref}}
			f.provider = &fakeProvider{payment: tc.payment}
			f.config = Config{IntervalRuleVerified: true}
			award := f.award("award-review")

			require.ErrorIs(t, f.dispatch(award, "award.fulfill"), ErrAwardNeedsReview)

			assert.Equal(t, "needs_review", f.db.GiveawayAward.GetX(t.Context(), award.ID).State)
			assert.Zero(t, f.users.ledger().commits, "an unprotected prize must never be committed")
			operation, err := f.db.BillingOperation.Query().Where(billingoperation.AwardIDEQ(award.ID)).Only(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "needs_review", operation.State)
		})
	}
}

func TestEngineReviewReopensAResolvedBillingAlert(t *testing.T) {
	f := newFixture(t)
	ref := "ref-1"
	next := f.now.Add(30 * 24 * time.Hour)
	f.users = &fakeUsers{coverage: users.PremiumCoverage{RecurringReference: &ref}}
	f.provider = &fakeProvider{payment: tebex.RecurringPayment{Reference: ref, Status: "Active", Interval: "P1M", NextPaymentDate: &next}}
	f.config = Config{IntervalRuleVerified: true}
	award := f.award("award-review")
	_, err := f.db.GiveawayAlert.Create().SetID(award.ID + ":billing").SetAwardID(award.ID).SetCategory("billing").SetState("resolved").SetMessage("old reason").
		SetResolvedAt(f.now.Add(-time.Hour)).SetAcknowledgedBy(7).SetAcknowledgedAt(f.now.Add(-time.Hour)).Save(t.Context())
	require.NoError(t, err)

	require.ErrorIs(t, f.dispatch(award, "award.fulfill"), ErrAwardNeedsReview)

	alert, err := f.db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(award.ID), giveawayalert.CategoryEQ("billing")).Only(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "unresolved", alert.State)
	assert.Equal(t, ErrAwardNeedsReview.Error(), alert.Message)
	assert.True(t, alert.ResolvedAt.IsZero())
	assert.True(t, alert.AcknowledgedAt.IsZero())
}
