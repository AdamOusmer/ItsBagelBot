// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/enttest"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/giveaways"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGiveawayRPCCampaignLifecycle(t *testing.T) {
	f := newGiveawayRPCFixture(t, giveawayengine.Config{NewAwardsEnabled: true})
	ctx := context.Background()
	pool := usersrpc.GiveawayPoolReply{Counts: usersrpc.GiveawayPoolCounts{Total: 4, Eligible: 2, Banned: 1, VIP: 1}, Candidates: []usersrpc.GiveawayCandidate{
		{UserID: 21, Username: "first", Status: "free", IsActive: true, Onboarded: true},
		{UserID: 22, Username: "second", Status: "paid", SubscriptionRef: strptr("recurring"), IsActive: true, Onboarded: true},
	}}
	f.pool(t, pool)
	request := giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "launch-create"}, Title: "Launch prizes", Reason: "community milestone", WinnerCount: 2, PrizeMonths: 3}
	created := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", request)
	require.Empty(t, created.Error)
	require.NotNil(t, created.Campaign)
	require.Equal(t, giveaways.StatusDraft, created.Campaign.Status)
	require.Equal(t, uint64(10), created.Campaign.CreatedBy)
	require.Equal(t, "giveaway-v1", created.Campaign.RulesVersion)
	replayedCreate := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", request)
	require.Equal(t, created, replayedCreate)
	require.Equal(t, 1, f.db.Giveaway.Query().CountX(ctx))

	preview := giveawayRequest[giveaways.PreviewReply](t, f.nc, giveaways.AdminPrefix+".preview", giveaways.PreviewRequest{Mutation: giveaways.Mutation{ActorID: "10"}, CampaignID: created.Campaign.ID})
	require.Empty(t, preview.Error)
	require.Equal(t, created.Campaign, preview.Campaign)
	require.Len(t, preview.Candidates, 2)
	require.Equal(t, "first", preview.Candidates[0].Username)
	require.True(t, preview.Candidates[0].Eligible)
	require.NotEmpty(t, preview.PoolDigest)
	require.Equal(t, 4, preview.Summary.Total)
	require.Equal(t, 2, preview.Summary.Excluded)
	require.Equal(t, 1, preview.Summary.Exclusions.Banned)
	require.Equal(t, "6", preview.Summary.TotalPrizeMonths)

	frozen := giveawayRequest[giveaways.FreezeReply](t, f.nc, giveaways.AdminPrefix+".freeze", giveaways.FreezeRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "launch-freeze", ExpectedVersion: created.Campaign.Version}, CampaignID: created.Campaign.ID, PoolDigest: preview.PoolDigest})
	require.Empty(t, frozen.Error)
	require.NotNil(t, frozen.Campaign)
	require.Equal(t, giveaways.StatusFrozen, frozen.Campaign.Status)
	require.Equal(t, created.Campaign.Version+1, frozen.Campaign.Version)
	require.NotNil(t, frozen.Campaign.FrozenAt)
	require.Equal(t, preview.Summary, frozen.Summary)

	drawRequest := giveaways.DrawRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "launch-draw", ExpectedVersion: frozen.Campaign.Version}, CampaignID: created.Campaign.ID, PoolDigest: preview.PoolDigest}
	drawn := giveawayRequest[giveaways.DrawReply](t, f.nc, giveaways.AdminPrefix+".draw", drawRequest)
	require.Empty(t, drawn.Error)
	require.NotNil(t, drawn.Draw)
	require.Equal(t, created.Campaign.ID, drawn.Draw.CampaignID)
	require.Equal(t, "launch-draw", drawn.Draw.OperationKey)
	require.Equal(t, preview.PoolDigest, drawn.Draw.PoolDigest)
	require.Equal(t, giveawayengine.DrawAlgorithmVersion, drawn.Draw.AlgorithmVersion)
	require.ElementsMatch(t, []uint64{21, 22}, drawn.Draw.WinnerIDs)
	require.Len(t, drawn.Awards, 2)
	for i, award := range drawn.Awards {
		require.Equal(t, drawn.Draw.ID, award.DrawID)
		require.Equal(t, drawn.Draw.WinnerIDs[i], award.UserID)
		require.Equal(t, uint64(i+1), award.Ordinal)
		require.Equal(t, 3, award.PrizeMonths)
		require.Equal(t, giveaways.AwardSelected, award.State)
		require.Equal(t, giveaways.BillingNotRequired, award.BillingState)
		require.Equal(t, giveaways.EmailQueued, award.EmailState)
		require.Nil(t, award.PlannedStart)
	}
	require.Equal(t, drawn, giveawayRequest[giveaways.DrawReply](t, f.nc, giveaways.AdminPrefix+".draw", drawRequest))
	require.Equal(t, 2, f.db.GiveawayAward.Query().CountX(ctx))
	require.Equal(t, 4, f.db.GiveawayOutbox.Query().CountX(ctx))

	f.assertDrawnCampaignReadback(t, frozen.Campaign, drawn, pool.Candidates[0])
}

func (f *giveawayRPCFixture) assertDrawnCampaignReadback(t *testing.T, frozen *giveaways.Campaign, drawn giveaways.DrawReply, firstCandidate usersrpc.GiveawayCandidate) {
	t.Helper()
	get := giveawayRequest[giveaways.GetReply](t, f.nc, giveaways.AdminPrefix+".get", giveaways.GetRequest{Mutation: giveaways.Mutation{ActorID: "10"}, CampaignID: frozen.ID})
	require.Empty(t, get.Error)
	require.NotNil(t, get.Campaign)
	require.Equal(t, giveaways.StatusDrawn, get.Campaign.Status)
	require.NotNil(t, get.Campaign.DrawnAt)
	require.Equal(t, frozen.Version+1, get.Campaign.Version)
	require.Equal(t, drawn.Draw, get.Draw)
	require.Equal(t, drawn.Awards, get.Awards)
	require.Len(t, get.Candidates, 2)
	require.Equal(t, []uint64{21, 22}, []uint64{get.Candidates[0].UserID, get.Candidates[1].UserID})
	var eligibility usersrpc.GiveawayCandidate
	facts, err := codec.Marshal(get.Candidates[0].Eligibility)
	require.NoError(t, err)
	require.NoError(t, codec.Unmarshal(facts, &eligibility))
	require.Equal(t, firstCandidate, eligibility)
	draft := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "next-create"}, Title: "Next campaign", WinnerCount: 1, PrizeMonths: 1})
	require.Empty(t, draft.Error)
	require.NotNil(t, draft.Campaign)
	require.Equal(t, giveaways.StatusDraft, draft.Campaign.Status)
	list := giveawayRequest[giveaways.ListReply](t, f.nc, giveaways.AdminPrefix+".list", giveaways.ListRequest{Mutation: giveaways.Mutation{ActorID: "10"}, Status: giveaways.StatusDrawn})
	require.Empty(t, list.Error)
	require.Equal(t, []giveaways.Campaign{*get.Campaign}, list.Campaigns)

	winner := drawn.Awards[0]
	userID := strconv.FormatUint(winner.UserID, 10)
	history := giveawayRequest[giveaways.HistoryReply](t, f.nc, giveaways.AdminPrefix+".history", giveaways.HistoryRequest{Mutation: giveaways.Mutation{ActorID: "10"}, UserID: userID})
	require.Empty(t, history.Error)
	require.Equal(t, []giveaways.Award{winner}, history.Awards)
	mine := giveawayRequest[giveaways.MineReply](t, f.nc, giveaways.MineSubject, giveaways.MineRequest{UserID: userID})
	require.Empty(t, mine.Error)
	require.Equal(t, history.Awards, mine.Awards)
	other := giveawayRequest[giveaways.MineReply](t, f.nc, giveaways.MineSubject, giveaways.MineRequest{UserID: "999"})
	require.Empty(t, other.Error)
	require.Empty(t, other.Awards)
}

func TestGiveawayRPCRejectedMutationsLeaveNoPersistedChanges(t *testing.T) {
	f := newGiveawayRPCFixture(t, giveawayengine.Config{})
	request := giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "bad-id", ActorRole: "owner", IdempotencyKey: "unauthorized"}, Title: "Launch prizes", WinnerCount: 1, PrizeMonths: 1}
	invalid := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", request)
	require.Equal(t, domainrpc.CodeInvalid, invalid.Code)
	require.NotEmpty(t, invalid.Error)
	require.Nil(t, invalid.Campaign)
	require.Zero(t, f.authCalls.Load())
	for _, actor := range []string{"20", "30"} {
		request.ActorID = actor
		denied := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", request)
		require.Equal(t, domainrpc.CodeForbidden, denied.Code)
		require.NotEmpty(t, denied.Error)
		require.Nil(t, denied.Campaign)
	}
	request.ActorID = "20"
	freeze := giveawayRequest[giveaways.FreezeReply](t, f.nc, giveaways.AdminPrefix+".freeze", giveaways.FreezeRequest{Mutation: request.Mutation, CampaignID: "missing", PoolDigest: "untrusted"})
	require.Equal(t, domainrpc.CodeForbidden, freeze.Code)
	draw := giveawayRequest[giveaways.DrawReply](t, f.nc, giveaways.AdminPrefix+".draw", giveaways.DrawRequest{Mutation: request.Mutation, CampaignID: "missing"})
	require.Equal(t, domainrpc.CodeForbidden, draw.Code)
	retry := giveawayRequest[giveaways.AwardRetryReply](t, f.nc, giveaways.AdminPrefix+".retry", giveaways.AwardRetryRequest{Mutation: request.Mutation, AwardID: "missing"})
	require.Equal(t, domainrpc.CodeForbidden, retry.Code)
	request.ActorID = "10"
	gated := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", request)
	require.Equal(t, domainrpc.CodeUnavailable, gated.Code)
	require.False(t, gated.Capabilities.NewAwardsEnabled)
	require.NotEmpty(t, gated.Capabilities.Reason)
	require.Zero(t, f.db.Giveaway.Query().CountX(context.Background()))
	require.Zero(t, f.db.GiveawayCandidate.Query().CountX(context.Background()))
	require.Zero(t, f.db.GiveawayAward.Query().CountX(context.Background()))
	invalidFreeze := giveawayRequest[giveaways.FreezeReply](t, f.nc, giveaways.AdminPrefix+".freeze", giveaways.FreezeRequest{Mutation: giveaways.Mutation{ActorID: "10"}, CampaignID: "missing"})
	require.Equal(t, domainrpc.CodeInvalid, invalidFreeze.Code)
	invalidPreview := giveawayRequest[giveaways.PreviewReply](t, f.nc, giveaways.AdminPrefix+".preview", giveaways.PreviewRequest{Mutation: giveaways.Mutation{ActorID: "10"}, WinnerCount: 1, PrizeMonths: 13})
	require.Equal(t, domainrpc.CodeInvalid, invalidPreview.Code)
}

func TestGiveawayRPCFrozenReplaySurvivesPoolOutage(t *testing.T) {
	f := newGiveawayRPCFixture(t, giveawayengine.Config{NewAwardsEnabled: true})
	poolSub := f.pool(t, usersrpc.GiveawayPoolReply{Counts: usersrpc.GiveawayPoolCounts{Total: 1, Eligible: 1}, Candidates: []usersrpc.GiveawayCandidate{{UserID: 21, Username: "winner", Status: "free"}}})
	created := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "create"}, Title: "Pool outage", WinnerCount: 1, PrizeMonths: 2})
	require.Empty(t, created.Error)
	preview := giveawayRequest[giveaways.PreviewReply](t, f.nc, giveaways.AdminPrefix+".preview", giveaways.PreviewRequest{Mutation: giveaways.Mutation{ActorID: "10"}, WinnerCount: 1, PrizeMonths: 2})
	require.Empty(t, preview.Error)
	require.Nil(t, preview.Campaign)
	require.Equal(t, 1, preview.Summary.RequestedWinners)
	require.Equal(t, 2, preview.Summary.PrizeMonths)
	request := giveaways.FreezeRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "freeze", ExpectedVersion: created.Campaign.Version}, CampaignID: created.Campaign.ID, PoolDigest: preview.PoolDigest}
	frozen := giveawayRequest[giveaways.FreezeReply](t, f.nc, giveaways.AdminPrefix+".freeze", request)
	require.Empty(t, frozen.Error)
	require.NoError(t, poolSub.Unsubscribe())
	require.NoError(t, f.nc.Flush())
	replayed := giveawayRequest[giveaways.FreezeReply](t, f.nc, giveaways.AdminPrefix+".freeze", request)
	require.Empty(t, replayed.Error)
	require.Equal(t, frozen.Campaign, replayed.Campaign)
	require.Equal(t, frozen.Summary.Eligible, replayed.Summary.Eligible)
	require.Equal(t, frozen.Summary.RequestedWinners, replayed.Summary.RequestedWinners)
	require.Equal(t, frozen.Summary.TotalPrizeMonths, replayed.Summary.TotalPrizeMonths)
	require.Equal(t, 1, f.db.GiveawayCandidate.Query().CountX(context.Background()))
	unavailable := giveawayRequest[giveaways.PreviewReply](t, f.nc, giveaways.AdminPrefix+".preview", giveaways.PreviewRequest{Mutation: giveaways.Mutation{ActorID: "10"}, CampaignID: created.Campaign.ID})
	require.Equal(t, domainrpc.CodeUnavailable, unavailable.Code)
	require.NotEmpty(t, unavailable.Error)
	require.Nil(t, unavailable.Campaign)
}

type giveawayRPCFixture struct {
	nc        *nats.Conn
	db        *ent.Client
	authCalls atomic.Int32
}

func newGiveawayRPCFixture(t *testing.T, config giveawayengine.Config) *giveawayRPCFixture {
	t.Helper()
	nc := testnats.Connect(t)
	db := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = db.Close() })
	f := &giveawayRPCFixture{nc: nc, db: db}
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop()}, defaultUsersAuthSubject, func(_ context.Context, req usersrpc.AuthRequest) usersrpc.AuthReply {
		f.authCalls.Add(1)
		if req.UserID == "30" {
			return usersrpc.AuthReply{Admin: true, Role: "staff"}
		}
		return usersrpc.AuthReply{Admin: req.UserID == "10", Role: "owner"}
	}))
	service := NewGiveawayRPC(GiveawayRPCConfig{DB: db, Store: giveawayengine.NewStore(db), Users: NewUsersGiveawayClient(nc), Config: config, RulesVersion: "giveaway-v1", Log: zap.NewNop()})
	require.NoError(t, SubscribeGiveaways(bus.RPCWiring{NC: nc, Queue: "giveaway-tests", Log: zap.NewNop()}, service))
	return f
}

func (f *giveawayRPCFixture) pool(t *testing.T, pool usersrpc.GiveawayPoolReply) *nats.Subscription {
	t.Helper()
	sub, err := f.nc.Subscribe(giveaways.UsersPoolSubject, func(msg *nats.Msg) { _ = bus.Respond(msg, pool) })
	require.NoError(t, err)
	require.NoError(t, f.nc.Flush())
	return sub
}

func giveawayRequest[T any](t *testing.T, nc *nats.Conn, subject string, request any) T {
	t.Helper()
	body, err := codec.Marshal(request)
	require.NoError(t, err)
	msg, err := nc.Request(subject, body, 3*time.Second)
	require.NoError(t, err)
	var reply T
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply
}

func strptr(value string) *string { return &value }

func TestGiveawayRPCCapabilitiesReflectLaunchGates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    giveawayengine.Config
		want      giveaways.Capabilities
		explained bool
	}{
		{
			name:   "reports every gate open without a reason",
			config: giveawayengine.Config{NewAwardsEnabled: true, IntervalRuleVerified: true, ProviderMutations: true},
			want:   giveaways.Capabilities{NewAwardsEnabled: true, SchedulingEnabled: true, ProviderMutations: true, IntervalRuleVerified: true},
		},
		{
			name:      "explains a launch with every gate closed",
			want:      giveaways.Capabilities{},
			explained: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGiveawayRPCFixture(t, tc.config)
			reply := giveawayRequest[giveaways.CapabilitiesReply](t, f.nc, giveaways.AdminPrefix+"."+giveaways.VerbCapabilities, giveaways.CapabilitiesRequest{Mutation: giveaways.Mutation{ActorID: "10"}})
			require.Empty(t, reply.Error)
			require.Equal(t, tc.explained, reply.Capabilities.Reason != "")
			reply.Capabilities.Reason = ""
			require.Equal(t, tc.want, reply.Capabilities)
		})
	}
}

func TestGiveawayRPCPreviewSummary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pool    usersrpc.GiveawayPoolReply
		winners int
		months  int
		want    giveaways.PoolSummary
	}{
		{
			name: "counts each candidate in exactly one category",
			pool: usersrpc.GiveawayPoolReply{Counts: usersrpc.GiveawayPoolCounts{Total: 6, Eligible: 6}, Candidates: []usersrpc.GiveawayCandidate{
				{UserID: 1, Status: "free"},
				{UserID: 2, Status: "paid"},
				{UserID: 3, Status: "paid"},
				{UserID: 4, Status: "paid", SubscriptionRef: strptr("recurring-4")},
				{UserID: 5, Status: "paid", SubscriptionRef: strptr("recurring-5")},
				{UserID: 6, Status: "free", SubscriptionRef: strptr("recurring-6")},
			}},
			winners: 1, months: 1,
			want: giveaways.PoolSummary{Total: 6, Eligible: 6, Free: 1, Premium: 2, Subscribers: 3, RequestedWinners: 1, PrizeMonths: 1, TotalPrizeMonths: "1"},
		},
		{
			name:    "carries the requested values and exclusions for an empty pool",
			pool:    usersrpc.GiveawayPoolReply{Counts: usersrpc.GiveawayPoolCounts{Total: 8, Eligible: 3, Banned: 2, VIP: 3}},
			winners: 5, months: 2,
			want: giveaways.PoolSummary{Total: 8, Eligible: 3, Excluded: 5, RequestedWinners: 5, PrizeMonths: 2, TotalPrizeMonths: "10", Exclusions: giveaways.PoolExclusionCounts{Banned: 2, VIP: 3}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGiveawayRPCFixture(t, giveawayengine.Config{})
			f.pool(t, tc.pool)
			preview := giveawayRequest[giveaways.PreviewReply](t, f.nc, giveaways.AdminPrefix+".preview", giveaways.PreviewRequest{Mutation: giveaways.Mutation{ActorID: "10"}, WinnerCount: tc.winners, PrizeMonths: tc.months})
			require.Empty(t, preview.Error)
			require.Equal(t, tc.want, preview.Summary)
		})
	}
}

func TestGiveawayRPCFrozenReplaySummarizesStoredExclusionsWithoutUsers(t *testing.T) {
	f := newGiveawayRPCFixture(t, giveawayengine.Config{NewAwardsEnabled: true})
	created := giveawayRequest[giveaways.CreateReply](t, f.nc, giveaways.AdminPrefix+".create", giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "create"}, Title: "Stored summary", WinnerCount: 2, PrizeMonths: 3})
	require.Empty(t, created.Error)
	candidates := []giveawayengine.Candidate{{UserID: 1, Eligible: true}, {UserID: 2, ExclusionReason: "vip"}, {UserID: 3, ExclusionReason: "current_staff"}}
	digest, err := giveawayengine.PoolDigest(candidates)
	require.NoError(t, err)
	freeze := giveaways.FreezeRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "freeze", ExpectedVersion: created.Campaign.Version}, CampaignID: created.Campaign.ID, PoolDigest: digest}
	_, err = giveawayengine.NewStore(f.db).FreezeCandidates(t.Context(), freeze, candidates, time.Now().UTC())
	require.NoError(t, err)

	replayed := giveawayRequest[giveaways.FreezeReply](t, f.nc, giveaways.AdminPrefix+".freeze", freeze)

	require.Empty(t, replayed.Error)
	require.Equal(t, giveaways.PoolSummary{Total: 3, Eligible: 1, Excluded: 2, RequestedWinners: 2, PrizeMonths: 3, TotalPrizeMonths: "6", Exclusions: giveaways.PoolExclusionCounts{VIP: 1, CurrentStaff: 1}}, replayed.Summary)
}
