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
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

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

func TestGiveawayCapabilitiesExplainDisabledScheduling(t *testing.T) {
	service := &GiveawayRPC{config: giveawayengine.Config{NewAwardsEnabled: true}}
	got := service.capabilities()
	require.False(t, got.SchedulingEnabled)
	require.Contains(t, got.Reason, "scheduling is disabled")
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
