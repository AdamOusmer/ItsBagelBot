package giveaway

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/ent/giveawaycandidate"
	"ItsBagelBot/app/db/transactions/ent/giveawayoutbox"
	"ItsBagelBot/internal/domain/rpc/giveaways"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestDrawReplayReturnsCommittedAwards(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-draw-replay"))
	t.Cleanup(func() { _ = client.Close() })
	store := NewStore(client)
	store.RandomReader = strings.NewReader("stable entropy for replay")
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	campaign, err := store.Create(ctx, giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "create-1"}, Title: "test", WinnerCount: 2, PrizeMonths: 3}, "giveaway-v1", now)
	require.NoError(t, err)
	candidates := []Candidate{{UserID: 7, Eligible: true}, {UserID: 2, Eligible: true}, {UserID: 9, Eligible: true}}
	digest, err := PoolDigest(candidates)
	require.NoError(t, err)
	_, err = store.FreezeCandidates(ctx, giveaways.FreezeRequest{Mutation: giveaways.Mutation{ExpectedVersion: campaign.Version}, CampaignID: campaign.ID, PoolDigest: digest}, candidates, now)
	require.NoError(t, err)
	first, err := store.Draw(ctx, giveaways.DrawRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "draw-1"}, CampaignID: campaign.ID, PoolDigest: digest}, now)
	require.NoError(t, err)
	second, err := store.Draw(ctx, giveaways.DrawRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "draw-retry"}, CampaignID: campaign.ID}, now)
	require.NoError(t, err)
	require.Equal(t, first.Draw.ID, second.Draw.ID)
	require.Len(t, second.Awards, len(first.Awards))
	for i := range first.Awards {
		require.Equal(t, first.Awards[i].UserID, second.Awards[i].UserID)
	}
	count, err := client.GiveawayOutbox.Query().Where(giveawayoutbox.AggregateIDEQ(first.Awards[0].ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestCreateAndFreezeReplayUseStableKeys(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-freeze-replay"))
	t.Cleanup(func() { _ = client.Close() })
	store := NewStore(client)
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	req := giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "create-stable"}, Title: "test", Reason: "launch", WinnerCount: 1, PrizeMonths: 2}
	first, err := store.Create(ctx, req, "giveaway-v1", now)
	require.NoError(t, err)
	replay, err := store.Create(ctx, req, "giveaway-v1", now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)
	changed := req
	changed.Reason = "different"
	_, err = store.Create(ctx, changed, "giveaway-v1", now)
	require.ErrorIs(t, err, ErrVersion)
	candidates := []Candidate{{UserID: 7, Eligible: true}, {UserID: 9, Eligible: false}}
	digest, err := PoolDigest(candidates)
	require.NoError(t, err)
	freeze := giveaways.FreezeRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "freeze-stable", ExpectedVersion: first.Version}, CampaignID: first.ID, PoolDigest: digest}
	frozen, err := store.FreezeCandidates(ctx, freeze, candidates, now)
	require.NoError(t, err)
	replayed, err := store.FreezeCandidates(ctx, freeze, candidates, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, frozen.ID, replayed.ID)
	count, err := client.GiveawayCandidate.Query().Where(giveawaycandidate.GiveawayIDEQ(first.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(candidates), count)
}

func TestRetryAwardPreservesLiveLeaseAndRequeuesExpiredWork(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN("giveaway-retry-lease"))
	t.Cleanup(func() { _ = client.Close() })
	store := NewStore(client)
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	_, err := client.GiveawayOutbox.Create().SetID("retry-live").SetAggregateID("award-retry").SetEventType("award.fulfill").SetPayloadJSON(`{"award_id":"award-retry"}`).SetState("processing").SetLeaseOwner("worker-a").SetLeaseUntil(future).Save(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, store.RetryAward(ctx, "award-retry", now), ErrAwardLeased)
	_, err = client.GiveawayOutbox.UpdateOneID("retry-live").SetLeaseUntil(now.Add(-time.Second)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, store.RetryAward(ctx, "award-retry", now))
	row := client.GiveawayOutbox.GetX(ctx, "retry-live")
	require.Equal(t, "queued", row.State)
	require.Empty(t, row.LeaseOwner)
	require.True(t, row.LeaseUntil.IsZero())
}
