// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/giveawaycandidate"
	"ItsBagelBot/app/db/transactions/ent/giveawayoutbox"
	"ItsBagelBot/internal/domain/rpc/giveaways"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDrawReplayReturnsCommittedAwards(t *testing.T) {
	f := newFixture(t)
	store := NewStore(f.db)
	store.RandomReader = strings.NewReader("stable entropy for replay")
	campaign, err := store.Create(t.Context(), giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "create-1"}, Title: "test", WinnerCount: 2, PrizeMonths: 3}, "giveaway-v1", f.now)
	require.NoError(t, err)
	candidates := []Candidate{{UserID: 7, Eligible: true}, {UserID: 2, Eligible: true}, {UserID: 9, Eligible: true}}
	digest, err := PoolDigest(candidates)
	require.NoError(t, err)
	_, err = store.FreezeCandidates(t.Context(), giveaways.FreezeRequest{Mutation: giveaways.Mutation{ExpectedVersion: campaign.Version}, CampaignID: campaign.ID, PoolDigest: digest}, candidates, f.now)
	require.NoError(t, err)

	first, err := store.Draw(t.Context(), giveaways.DrawRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "draw-1"}, CampaignID: campaign.ID, PoolDigest: digest}, f.now)
	require.NoError(t, err)
	second, err := store.Draw(t.Context(), giveaways.DrawRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "draw-retry"}, CampaignID: campaign.ID}, f.now)
	require.NoError(t, err)

	assert.Equal(t, first.Draw.ID, second.Draw.ID)
	require.Len(t, second.Awards, len(first.Awards))
	for i := range first.Awards {
		assert.Equal(t, first.Awards[i].UserID, second.Awards[i].UserID)
	}
	count, err := f.db.GiveawayOutbox.Query().Where(giveawayoutbox.AggregateIDEQ(first.Awards[0].ID)).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestCreateAndFreezeReplayUseStableKeys(t *testing.T) {
	f := newFixture(t)
	store := NewStore(f.db)
	req := giveaways.CreateRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "create-stable"}, Title: "test", Reason: "launch", WinnerCount: 1, PrizeMonths: 2}
	first, err := store.Create(t.Context(), req, "giveaway-v1", f.now)
	require.NoError(t, err)
	replay, err := store.Create(t.Context(), req, "giveaway-v1", f.now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, first.ID, replay.ID)
	changed := req
	changed.Reason = "different"
	_, err = store.Create(t.Context(), changed, "giveaway-v1", f.now)
	require.ErrorIs(t, err, ErrVersion)

	candidates := []Candidate{{UserID: 7, Eligible: true}, {UserID: 9, Eligible: false}}
	digest, err := PoolDigest(candidates)
	require.NoError(t, err)
	freeze := giveaways.FreezeRequest{Mutation: giveaways.Mutation{ActorID: "10", IdempotencyKey: "freeze-stable", ExpectedVersion: first.Version}, CampaignID: first.ID, PoolDigest: digest}
	frozen, err := store.FreezeCandidates(t.Context(), freeze, candidates, f.now)
	require.NoError(t, err)
	replayed, err := store.FreezeCandidates(t.Context(), freeze, candidates, f.now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, frozen.ID, replayed.ID)
	count, err := f.db.GiveawayCandidate.Query().Where(giveawaycandidate.GiveawayIDEQ(first.ID)).Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, len(candidates), count)
}

func TestRetryAwardPreservesLiveLeaseAndRequeuesExpiredWork(t *testing.T) {
	f := newFixture(t)
	store := NewStore(f.db)
	_, err := f.db.GiveawayOutbox.Create().SetID("retry-live").SetAggregateID("award-retry").SetEventType("award.fulfill").SetPayloadJSON(`{"award_id":"award-retry"}`).SetState("processing").SetLeaseOwner("worker-a").SetLeaseUntil(f.now.Add(time.Minute)).Save(t.Context())
	require.NoError(t, err)
	require.ErrorIs(t, store.RetryAward(t.Context(), "award-retry", f.now), ErrAwardLeased)

	_, err = f.db.GiveawayOutbox.UpdateOneID("retry-live").SetLeaseUntil(f.now.Add(-time.Second)).Save(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.RetryAward(t.Context(), "award-retry", f.now))

	row := f.db.GiveawayOutbox.GetX(t.Context(), "retry-live")
	assert.Equal(t, "queued", row.State)
	assert.Empty(t, row.LeaseOwner)
	assert.True(t, row.LeaseUntil.IsZero())
}
