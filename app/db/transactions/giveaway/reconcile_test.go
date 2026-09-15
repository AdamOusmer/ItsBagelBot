package giveaway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/tebex"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type reconcileProvider struct {
	payment tebex.RecurringPayment
	calls   int
}

type reconciliationUsers struct {
	coverage usersrpc.PremiumCoverage
}

func (u reconciliationUsers) Pool(context.Context, *time.Time) (usersrpc.GiveawayPoolReply, error) {
	return usersrpc.GiveawayPoolReply{}, nil
}
func (u reconciliationUsers) Coverage(context.Context, uint64) (usersrpc.PremiumCoverage, error) {
	return u.coverage, nil
}
func (u reconciliationUsers) Prepare(context.Context, usersrpc.PreparePremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	return usersrpc.PremiumGrant{}, nil
}
func (u reconciliationUsers) Commit(context.Context, usersrpc.CommitPremiumGrantRequest) (usersrpc.PremiumGrant, error) {
	return usersrpc.PremiumGrant{}, nil
}
func (u reconciliationUsers) Email(context.Context, uint64) (string, error) { return "", nil }

func (p *reconcileProvider) GetRecurring(context.Context, string) (tebex.RecurringPayment, error) {
	p.calls++
	return p.payment, nil
}

func (p *reconcileProvider) PauseRecurring(context.Context, string, time.Time) (tebex.RecurringPayment, error) {
	return p.payment, nil
}

type reconciliationAwardSpec struct {
	id, state, billingState, ref string
	userID                       uint64
	now, end                     time.Time
}

func seedReconciliationAward(t *testing.T, client *ent.Client, spec reconciliationAwardSpec) *ent.GiveawayAward {
	t.Helper()
	award, err := client.GiveawayAward.Create().SetID(spec.id).SetGiveawayID("campaign").SetDrawID("draw").SetUserID(spec.userID).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("verified").SetState(spec.state).SetBillingState(spec.billingState).SetPlannedStart(spec.now).SetPlannedEnd(spec.end).Save(context.Background())
	require.NoError(t, err)
	_, err = client.BillingOperation.Create().SetID("billing:" + award.ID).SetAwardID(award.ID).SetAgreementID(fmt.Sprintf("agreement:%d:%s", spec.userID, spec.ref)).SetRecurringReference(spec.ref).SetRequestedStart(spec.now).SetRequestedEnd(spec.end).SetUpdatedAt(spec.now.Add(-time.Hour)).Save(context.Background())
	require.NoError(t, err)
	return award
}

func TestProviderReconciliationPersistsAgreementAndClearsLease(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-provider-reconcile"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	end := now.Add(2 * time.Hour)
	ref := "ref-1"
	paused := now.Add(24 * time.Hour)
	next := now.Add(30 * time.Minute)
	award := seedReconciliationAward(t, client, reconciliationAwardSpec{id: "award-reconcile", userID: 7, state: "scheduled", billingState: "protected", ref: ref, now: now, end: end})
	provider := &reconcileProvider{payment: tebex.RecurringPayment{Reference: ref, Status: "Paused", Interval: "P1M", NextPaymentDate: &next, PausedUntil: &paused}}
	engine := NewEngine(EngineConfig{Store: NewStore(client), Provider: provider, Config: Config{ReconcileInterval: 15 * time.Minute, BoundaryInterval: time.Minute}, Now: func() time.Time { return now }})
	require.NoError(t, engine.reconcileProviders(context.Background()))
	require.Equal(t, 1, provider.calls)
	agreement, err := client.TebexAgreement.Get(context.Background(), "agreement:7:"+ref)
	require.NoError(t, err)
	require.Equal(t, "Paused", agreement.ProviderStatus)
	require.False(t, agreement.PausedUntil.IsZero())
	require.False(t, agreement.VerifiedAt.IsZero())
	operation, err := client.BillingOperation.Get(context.Background(), "billing:"+award.ID)
	require.NoError(t, err)
	require.Equal(t, "verified", operation.State)
	require.True(t, operation.LeaseUntil.IsZero())
}

func TestProviderReconciliationAlertsOnCancellationWithoutRevokingAward(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-provider-drift"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	end := now.Add(2 * time.Hour)
	ref := "ref-2"
	award := seedReconciliationAward(t, client, reconciliationAwardSpec{id: "award-drift", userID: 8, state: "active", billingState: "protected", ref: ref, now: now, end: end})
	provider := &reconcileProvider{payment: tebex.RecurringPayment{Reference: ref, Status: "Paused", Interval: "P1M", CancellationRequested: true}}
	engine := NewEngine(EngineConfig{Store: NewStore(client), Provider: provider, Config: Config{ReconcileInterval: time.Minute, BoundaryInterval: time.Minute}, Now: func() time.Time { return now }})
	require.NoError(t, engine.reconcileProviders(context.Background()))
	updated := client.GiveawayAward.GetX(context.Background(), award.ID)
	require.Equal(t, "active", updated.State)
	operation, err := client.BillingOperation.Get(context.Background(), "billing:"+award.ID)
	require.NoError(t, err)
	require.Equal(t, "needs_review", operation.State)
	exists, err := client.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ(award.ID), giveawayalert.CategoryEQ("billing-reconciliation")).Exist(context.Background())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestProviderReconciliationStopsBeforeProbeWhenCancellationIsPending(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-cancel-pending"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	end := now.Add(2 * time.Hour)
	ref := "ref-cancel-pending"
	award := seedReconciliationAward(t, client, reconciliationAwardSpec{id: "award-cancel-pending", userID: 9, state: "active", billingState: "protected", ref: ref, now: now, end: end})
	provider := &reconcileProvider{payment: tebex.RecurringPayment{Reference: ref, Status: "Paused"}}
	engine := NewEngine(EngineConfig{
		Store:    NewStore(client),
		Users:    reconciliationUsers{coverage: usersrpc.PremiumCoverage{RecurringReference: &ref, CancelPending: true}},
		Provider: provider,
		Config:   Config{ReconcileInterval: time.Minute, BoundaryInterval: time.Minute},
		Now:      func() time.Time { return now },
	})
	require.NoError(t, engine.reconcileProviders(context.Background()))
	require.Equal(t, 0, provider.calls)
	updated := client.GiveawayAward.GetX(context.Background(), award.ID)
	require.Equal(t, "uncertain", updated.BillingState)
}
