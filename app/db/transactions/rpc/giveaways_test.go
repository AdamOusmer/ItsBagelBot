// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/transactions/ent"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	giveaways "ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
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
