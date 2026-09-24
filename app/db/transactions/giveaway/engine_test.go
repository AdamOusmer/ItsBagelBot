package giveaway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/billingoperation"
	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/ent/giveawayfulfillmentplan"
	"ItsBagelBot/app/db/transactions/tebex"
	users "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type fakeUsers struct {
	mu                sync.Mutex
	prepares, commits int
	lastPrepare       users.PreparePremiumGrantRequest
	coverage          users.PremiumCoverage
	commitErr         error
}

func (f *fakeUsers) Pool(context.Context, *time.Time) (users.GiveawayPoolReply, error) {
	return users.GiveawayPoolReply{}, nil
}
func (f *fakeUsers) Coverage(context.Context, uint64) (users.PremiumCoverage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.coverage, nil
}
func (f *fakeUsers) Prepare(_ context.Context, req users.PreparePremiumGrantRequest) (users.PremiumGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepares++
	f.lastPrepare = req
	return users.PremiumGrant{ID: 7}, nil
}
func (f *fakeUsers) Commit(context.Context, users.CommitPremiumGrantRequest) (users.PremiumGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	if f.commitErr != nil {
		return users.PremiumGrant{}, f.commitErr
	}
	return users.PremiumGrant{ID: 7}, nil
}
func (f *fakeUsers) Email(context.Context, uint64) (string, error) { return "", nil }

func TestEngineRestartDoesNotDuplicatePreparedGrant(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-engine"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	award, err := client.GiveawayAward.Create().SetID("award-1").SetGiveawayID("campaign-1").SetDrawID("draw-1").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(now).Save(context.Background())
	require.NoError(t, err)
	payload := `{"award_id":"award-1"}`
	_, err = client.GiveawayOutbox.Create().SetID("work-1").SetAggregateID(award.ID).SetEventType("award.fulfill").SetPayloadJSON(payload).Save(context.Background())
	require.NoError(t, err)
	usersPort := &fakeUsers{}
	cfg := EngineConfig{Store: NewStore(client), Users: usersPort, Config: Config{PromotionalGrantsEnabled: true}, Now: func() time.Time { return now }}
	require.NoError(t, NewEngine(cfg).DispatchOnce(context.Background()))
	require.Error(t, NewEngine(cfg).DispatchOnce(context.Background()))
	usersPort.mu.Lock()
	prepares, commits, rule := usersPort.prepares, usersPort.commits, usersPort.lastPrepare.IntervalRuleVersion
	preparedStart, preparedEnd := usersPort.lastPrepare.StartAt, usersPort.lastPrepare.EndAt
	usersPort.mu.Unlock()
	require.Equal(t, 1, prepares)
	require.Equal(t, 1, commits)
	require.Equal(t, PromotionalCalendarMonthRule, rule)
	require.Equal(t, now, preparedStart)
	require.Equal(t, now.AddDate(0, 1, 0), preparedEnd)
	row := client.GiveawayAward.GetX(context.Background(), award.ID)
	require.Equal(t, "active", row.State)
	plan, err := client.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, PromotionalCalendarMonthRule, plan.IntervalRule)
	require.Equal(t, now, plan.StartAt)
	require.Equal(t, now.AddDate(0, 1, 0), plan.EndAt)
}

func TestEngineCommitRetryReusesImmutablePlanAndResolvesFulfillmentAlert(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-plan-retry"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	award, err := client.GiveawayAward.Create().SetID("award-plan-retry").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(now).Save(context.Background())
	require.NoError(t, err)
	_, err = client.GiveawayAlert.Create().SetID(award.ID + ":fulfillment").SetAwardID(award.ID).SetCategory("fulfillment").SetState("unresolved").SetMessage("old commit failure").Save(context.Background())
	require.NoError(t, err)
	_, err = client.GiveawayOutbox.Create().SetID("work-plan-retry").SetAggregateID(award.ID).SetEventType("award.fulfill").SetPayloadJSON(`{"award_id":"award-plan-retry"}`).Save(context.Background())
	require.NoError(t, err)
	commitErr := errors.New("temporary users commit failure")
	usersPort := &fakeUsers{commitErr: commitErr}
	cfg := EngineConfig{Store: NewStore(client), Users: usersPort, Config: Config{PromotionalGrantsEnabled: true}, Now: func() time.Time { return now }}
	engine := NewEngine(cfg)
	require.ErrorIs(t, engine.DispatchOnce(context.Background()), commitErr)
	firstPlan, err := client.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(context.Background())
	require.NoError(t, err)
	firstAward := client.GiveawayAward.GetX(context.Background(), award.ID)
	usersPort.mu.Lock()
	usersPort.commitErr = nil
	usersPort.mu.Unlock()
	_, err = client.GiveawayOutbox.UpdateOneID("work-plan-retry").SetState("queued").ClearNextAttemptAt().Save(context.Background())
	require.NoError(t, err)
	require.NoError(t, engine.DispatchOnce(context.Background()))
	secondPlan, err := client.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(award.ID)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, firstPlan.IntervalRule, secondPlan.IntervalRule)
	require.Equal(t, firstPlan.StartAt, secondPlan.StartAt)
	require.Equal(t, firstPlan.EndAt, secondPlan.EndAt)
	updated := client.GiveawayAward.GetX(context.Background(), award.ID)
	require.Equal(t, "provider-monthly-unverified", updated.IntervalRule)
	require.Equal(t, "active", updated.State)
	require.Empty(t, updated.FailureReason)
	require.Equal(t, firstAward.PlannedStart, updated.PlannedStart)
	require.Equal(t, firstAward.PlannedEnd, updated.PlannedEnd)
	usersPort.mu.Lock()
	preparedRule := usersPort.lastPrepare.IntervalRuleVersion
	preparedStart, preparedEnd := usersPort.lastPrepare.StartAt, usersPort.lastPrepare.EndAt
	usersPort.mu.Unlock()
	require.Equal(t, PromotionalCalendarMonthRule, preparedRule)
	require.Equal(t, firstPlan.StartAt, preparedStart)
	require.Equal(t, firstPlan.EndAt, preparedEnd)
	alert, err := client.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(award.ID), giveawayalert.CategoryEQ("fulfillment")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "resolved", alert.State)
}

func TestProviderInspectDoesNotTrustAnUnownedPause(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-unowned-pause"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	end := now.Add(2 * time.Hour)
	ref := "ref-unowned"
	_, err := client.BillingOperation.Create().SetID("billing:unowned").SetAwardID("award-unowned").SetAgreementID("agreement:unowned").SetRecurringReference(ref).SetRequestedStart(now).SetRequestedEnd(end).Save(context.Background())
	require.NoError(t, err)
	next := now.Add(time.Hour)
	paused := now.Add(24 * time.Hour)
	adapter := engineProviderAdapter{
		provider: &reconcileProvider{payment: tebex.RecurringPayment{Reference: ref, Status: "Paused", Interval: "P1M", NextPaymentDate: &next, PausedUntil: &paused}},
		db:       client, awardID: "award-unowned",
	}
	state, err := adapter.Inspect(context.Background(), ref)
	require.NoError(t, err)
	require.True(t, state.Ambiguous)
	require.Nil(t, state.ProtectedUntil)

	operation, err := client.BillingOperation.Query().Where(billingoperation.IDEQ("billing:unowned")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "pending", operation.State)
}

func TestEngineKeepsRecurringWinnerPendingUntilProviderRuleVerified(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-provider-rule-gate"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	ref := "recurring-ref"
	award, err := client.GiveawayAward.Create().SetID("award-provider-gate").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(now).Save(context.Background())
	require.NoError(t, err)
	_, err = client.GiveawayOutbox.Create().SetID("work-provider-gate").SetAggregateID(award.ID).SetEventType("award.fulfill").SetPayloadJSON(`{"award_id":"award-provider-gate"}`).Save(context.Background())
	require.NoError(t, err)
	usersPort := &fakeUsers{coverage: users.PremiumCoverage{RecurringReference: &ref}}
	engine := NewEngine(EngineConfig{Store: NewStore(client), Users: usersPort, Config: Config{PromotionalGrantsEnabled: true}, Now: func() time.Time { return now }})
	require.ErrorIs(t, engine.DispatchOnce(context.Background()), ErrAwardNeedsReview)
	updated := client.GiveawayAward.GetX(context.Background(), award.ID)
	require.Equal(t, "needs_review", updated.State)
	usersPort.mu.Lock()
	prepares := usersPort.prepares
	usersPort.mu.Unlock()
	require.Equal(t, 0, prepares)
}

func TestEngineDoesNotBypassDisabledRuleForSavedPlan(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-saved-plan-gate"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	award, err := client.GiveawayAward.Create().SetID("award-saved-plan-gate").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule(PromotionalCalendarMonthRule).SetPlannedStart(now).SetPlannedEnd(now.AddDate(0, 1, 0)).Save(context.Background())
	require.NoError(t, err)
	engine := NewEngine(EngineConfig{Store: NewStore(client), Now: func() time.Time { return now }})
	_, err = engine.planAward(context.Background(), award, users.PremiumCoverage{})
	require.ErrorIs(t, err, ErrAwardNeedsReview)
	updated := client.GiveawayAward.GetX(context.Background(), award.ID)
	require.Equal(t, "needs_review", updated.State)
}

func TestPersistReviewAlertReopensResolvedAlert(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-review-alert"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	_, err := client.GiveawayAlert.Create().SetID("award-review:billing").SetAwardID("award-review").SetCategory("billing").SetState("resolved").SetMessage("old reason").SetResolvedAt(now.Add(-time.Hour)).SetAcknowledgedBy(7).SetAcknowledgedAt(now.Add(-time.Hour)).Save(context.Background())
	require.NoError(t, err)
	require.NoError(t, persistReviewAlert(context.Background(), client, "award-review", errors.New("new reason")))
	alert, err := client.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ("award-review"), giveawayalert.CategoryEQ("billing")).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, "unresolved", alert.State)
	require.Equal(t, "new reason", alert.Message)
	require.True(t, alert.ResolvedAt.IsZero())
	require.True(t, alert.AcknowledgedAt.IsZero())
}
