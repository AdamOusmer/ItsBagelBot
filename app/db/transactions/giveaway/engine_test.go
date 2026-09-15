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
	"ItsBagelBot/app/db/transactions/tebex"
	users "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type fakeUsers struct {
	mu                sync.Mutex
	prepares, commits int
}

func (f *fakeUsers) Pool(context.Context, *time.Time) (users.GiveawayPoolReply, error) {
	return users.GiveawayPoolReply{}, nil
}
func (f *fakeUsers) Coverage(context.Context, uint64) (users.PremiumCoverage, error) {
	return users.PremiumCoverage{}, nil
}
func (f *fakeUsers) Prepare(context.Context, users.PreparePremiumGrantRequest) (users.PremiumGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepares++
	return users.PremiumGrant{ID: 7}, nil
}
func (f *fakeUsers) Commit(context.Context, users.CommitPremiumGrantRequest) (users.PremiumGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	return users.PremiumGrant{ID: 7}, nil
}
func (f *fakeUsers) Email(context.Context, uint64) (string, error) { return "", nil }

func TestEngineRestartDoesNotDuplicatePreparedGrant(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-engine"))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	award, err := client.GiveawayAward.Create().SetID("award-1").SetGiveawayID("campaign-1").SetDrawID("draw-1").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(now).Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"award_id":"award-1"}`
	if _, err = client.GiveawayOutbox.Create().SetID("work-1").SetAggregateID(award.ID).SetEventType("award.fulfill").SetPayloadJSON(payload).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	usersPort := &fakeUsers{}
	cfg := EngineConfig{Store: NewStore(client), Users: usersPort, Config: Config{IntervalRuleVerified: true}, Now: func() time.Time { return now }}
	if err = NewEngine(cfg).DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A fresh Engine instance sees the durable completed work and cannot issue
	// another grant, even though all process-local state was discarded.
	if err = NewEngine(cfg).DispatchOnce(context.Background()); err == nil {
		t.Fatal("expected no work after restart")
	}
	usersPort.mu.Lock()
	prepares, commits := usersPort.prepares, usersPort.commits
	usersPort.mu.Unlock()
	if prepares != 1 || commits != 1 {
		t.Fatalf("grant calls prepare=%d commit=%d", prepares, commits)
	}
	row := client.GiveawayAward.GetX(context.Background(), award.ID)
	if row.State != "active" {
		t.Fatalf("state=%q", row.State)
	}
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
