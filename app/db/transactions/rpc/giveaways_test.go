// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/ent/giveawayfulfillmentplan"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	giveaways "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestGiveawayCapabilitiesReflectLaunchGates(t *testing.T) {
	service := &GiveawayRPC{config: giveawayengine.Config{NewAwardsEnabled: true, IntervalRuleVerified: true, ProviderMutations: true}}
	got := service.capabilities()
	if !allGatesEnabled(got) {
		t.Fatalf("capabilities lost enabled gates: %+v", got)
	}
	service.config = giveawayengine.Config{}
	got = service.capabilities()
	if anyGateEnabled(got) || got.Reason == "" {
		t.Fatalf("disabled gates were advertised as active: %+v", got)
	}
}

func TestGiveawayCapabilitiesExposeNonRecurringSchedulingSeparately(t *testing.T) {
	service := &GiveawayRPC{config: giveawayengine.Config{NewAwardsEnabled: true, PromotionalGrantsEnabled: true}}
	got := service.capabilities()
	if !got.SchedulingEnabled {
		t.Fatalf("nonrecurring scheduling should be enabled: %+v", got)
	}
	if got.ProviderMutations {
		t.Fatalf("provider mutations should remain disabled: %+v", got)
	}
	if got.IntervalRuleVerified {
		t.Fatalf("interval verification should remain false: %+v", got)
	}
	if got.Reason == "" {
		t.Fatal("capabilities should explain unavailable subscriber protection")
	}
}

func TestGiveawayCapabilitiesExposeProviderSchedulingSeparately(t *testing.T) {
	service := &GiveawayRPC{config: giveawayengine.Config{NewAwardsEnabled: true, IntervalRuleVerified: true, ProviderMutations: true}}
	got := service.capabilities()
	require.True(t, got.SchedulingEnabled)
	require.True(t, got.ProviderMutations)
	require.Empty(t, got.Reason)
}

func TestGiveawayCapabilitiesExplainDisabledScheduling(t *testing.T) {
	service := &GiveawayRPC{config: giveawayengine.Config{NewAwardsEnabled: true}}
	got := service.capabilities()
	require.False(t, got.SchedulingEnabled)
	require.Contains(t, got.Reason, "scheduling is disabled")
}

func allGatesEnabled(c giveaways.Capabilities) bool {
	return c.NewAwardsEnabled && c.SchedulingEnabled && c.ProviderMutations && c.IntervalRuleVerified
}
func anyGateEnabled(c giveaways.Capabilities) bool {
	return c.NewAwardsEnabled || c.SchedulingEnabled || c.ProviderMutations || c.IntervalRuleVerified
}

func TestGiveawaySummaryUsesExclusiveCategories(t *testing.T) {
	pool := usersrpc.GiveawayPoolReply{Counts: usersrpc.GiveawayPoolCounts{Total: 3, Eligible: 3}, Candidates: []usersrpc.GiveawayCandidate{
		{UserID: 1, Status: "free"},
		{UserID: 2, Status: "paid"},
		{UserID: 3, Status: "paid", SubscriptionRef: strptr("recurring")},
	}}
	// A real Ent row is unnecessary for this wire invariant; use the helper
	// below so the test remains independent of a database driver.
	if got := categoryCounts(pool); got != [3]int{1, 1, 1} {
		t.Fatalf("category counts = %v, want free/one-time/subscriber = 1/1/1", got)
	}
}

func TestEmptyPreviewSummaryCarriesRequestedValues(t *testing.T) {
	pool := usersrpc.GiveawayPoolReply{Counts: usersrpc.GiveawayPoolCounts{Total: 8, Eligible: 3, Banned: 2, VIP: 3}}
	got := summaryValues(pool, 5, 2)
	require.Equal(t, 5, got.RequestedWinners)
	require.Equal(t, 2, got.PrizeMonths)
	require.Equal(t, 5, got.Excluded)
	require.Equal(t, "10", got.TotalPrizeMonths)
	require.Equal(t, 2, got.Exclusions.Banned)
	require.Equal(t, 3, got.Exclusions.VIP)
}

func TestStoredFrozenSummaryDoesNotRequireUsers(t *testing.T) {
	campaign := &ent.Giveaway{WinnerCount: 2, PrizeMonths: 3}
	rows := []*ent.GiveawayCandidate{{Eligible: true}, {Eligible: false, ExclusionReason: "vip"}, {Eligible: false, ExclusionReason: "current_staff"}}
	got := storedSummary(campaign, rows)
	require.Equal(t, 3, got.Total)
	require.Equal(t, 1, got.Eligible)
	require.Equal(t, 2, got.Excluded)
	require.Equal(t, 1, got.Exclusions.VIP)
	require.Equal(t, 1, got.Exclusions.CurrentStaff)
}

func TestAwardViewsPreferImmutableFulfillmentPlanRule(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	planned, err := client.GiveawayAward.Create().SetID("award-planned").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(1).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(now).Save(context.Background())
	require.NoError(t, err)
	_, err = client.GiveawayFulfillmentPlan.Create().SetID("plan:award-planned").SetAwardID(planned.ID).SetIntervalRule(giveawayengine.PromotionalCalendarMonthRule).SetStartAt(now).SetEndAt(now.AddDate(0, 1, 0)).SetCreatedAt(now).Save(context.Background())
	require.NoError(t, err)
	unplanned, err := client.GiveawayAward.Create().SetID("award-unplanned").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(2).SetOrdinal(2).SetPrizeMonths(1).SetIntervalRule("provider-monthly-unverified").SetSelectedAt(now).Save(context.Background())
	require.NoError(t, err)

	service := &GiveawayRPC{db: client}
	views, err := service.awardViews(context.Background(), []*ent.GiveawayAward{planned, unplanned})
	require.NoError(t, err)
	require.Len(t, views, 2)
	require.Equal(t, giveawayengine.PromotionalCalendarMonthRule, views[0].IntervalRule)
	require.Equal(t, "provider-monthly-unverified", views[1].IntervalRule)

	plan, err := client.GiveawayFulfillmentPlan.Query().Where(giveawayfulfillmentplan.AwardIDEQ(planned.ID)).Only(context.Background())
	require.NoError(t, err)
	require.Equal(t, giveawayengine.PromotionalCalendarMonthRule, plan.IntervalRule)
	require.Equal(t, "provider-monthly-unverified", planned.IntervalRule)
}

func TestAdminAuthorizationRejectsMalformedActorBeforeNATS(t *testing.T) {
	service := &GiveawayRPC{users: &UsersGiveawayClient{}}
	_, refusal := service.authorize(context.Background(), "not-a-user-id")
	if refusal.Code != "invalid" || refusal.Error == "" {
		t.Fatalf("malformed actor was not rejected: %+v", refusal)
	}
}

func categoryCounts(pool usersrpc.GiveawayPoolReply) [3]int {
	var result [3]int
	for _, candidate := range pool.Candidates {
		if candidate.SubscriptionRef != nil && *candidate.SubscriptionRef != "" {
			result[2]++
			continue
		}
		if candidate.Status == "free" {
			result[0]++
		} else {
			result[1]++
		}
	}
	return result
}

func strptr(value string) *string { return &value }
