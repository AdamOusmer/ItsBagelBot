// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/internal/domain/overlays/feed"

	"github.com/stretchr/testify/require"
)

func cheerDelta(viewer string, bits int64) feed.Delta {
	return feed.DeltaOf(channelevents.Event{
		Kind: channelevents.Cheer, Amount: bits, User: &channelevents.User{ID: viewer, Name: "Viewer " + viewer},
	})
}

func TestFeedTotalsAccumulateAcrossEvents(t *testing.T) {
	repo, _ := setupStore(t)
	ctx := t.Context()

	for i, step := range []feed.Delta{cheerDelta("7", 100), cheerDelta("7", 50), cheerDelta("8", 120)} {
		require.NoError(t, repo.AddFeedTotals(ctx, userID, "evt-"+strconv.Itoa(i), step))
	}
	totals, err := repo.FeedTotals(ctx, userID)
	require.NoError(t, err)

	require.Equal(t, int64(270), totals.Counts.Bits)
	require.Equal(t, feed.Leader{User: feed.User{ID: "7", Name: "Viewer 7"}, Amount: 150}, *totals.TopCheerer)
	require.Nil(t, totals.TopGifter)
}

func TestFeedTotalsCountAnEventIdOnce(t *testing.T) {
	repo, _ := setupStore(t)
	ctx := t.Context()

	for range 3 {
		require.NoError(t, repo.AddFeedTotals(ctx, userID, "evt-1", cheerDelta("7", 100)))
	}
	require.NoError(t, repo.AddFeedTotals(ctx, userID, "evt-2", cheerDelta("7", 50)))
	totals, err := repo.FeedTotals(ctx, userID)

	require.NoError(t, err)
	require.Equal(t, int64(150), totals.Counts.Bits)
	require.Equal(t, int64(150), totals.TopCheerer.Amount)
}

func TestPurgedReceiptsLetAnOldEventIdCountAgain(t *testing.T) {
	repo, _ := setupStore(t)
	ctx := t.Context()
	require.NoError(t, repo.AddFeedTotals(ctx, userID, "evt-1", cheerDelta("7", 100)))

	require.NoError(t, repo.PurgeFeedReceipts(ctx, userID, time.Now().Add(-time.Hour)))
	require.NoError(t, repo.AddFeedTotals(ctx, userID, "evt-1", cheerDelta("7", 100)))
	kept, err := repo.FeedTotals(ctx, userID)
	require.NoError(t, err)
	require.NoError(t, repo.PurgeFeedReceipts(ctx, userID, time.Now().Add(time.Hour)))
	require.NoError(t, repo.AddFeedTotals(ctx, userID, "evt-1", cheerDelta("7", 100)))
	counted, err := repo.FeedTotals(ctx, userID)

	require.NoError(t, err)
	require.Equal(t, int64(100), kept.Counts.Bits)
	require.Equal(t, int64(200), counted.Counts.Bits)
}

func TestFeedTotalsAreScopedToTheChannel(t *testing.T) {
	repo, _ := setupStore(t)
	require.NoError(t, repo.AddFeedTotals(t.Context(), userID, "evt-1", cheerDelta("7", 100)))

	other, err := repo.FeedTotals(t.Context(), strangerID)

	require.NoError(t, err)
	require.Zero(t, other.Counts.Bits)
}

func TestFeedProfileFollowsOverlayOwnershipAndSubathonRules(t *testing.T) {
	repo, _ := setupStore(t)
	ctx := t.Context()
	before, err := repo.FeedProfile(ctx, userID)
	require.NoError(t, err)
	doc := customDoc("none")
	doc.Assets = nil
	doc.Widgets = []overlays.Widget{{
		ID: "t1", Name: "Timer", Kind: overlays.WidgetTimer, W: 100, H: 100, Opacity: 1,
		Settings: map[string]any{"mode": "subathon", "rules": map[string]any{"follow_seconds": 10, "tier1_seconds": 300}},
	}}
	_, _, err = repo.CreateCustom(ctx, userID, doc)
	require.NoError(t, err)

	after, err := repo.FeedProfile(ctx, userID)
	require.NoError(t, err)

	require.False(t, before.Owned)
	require.Equal(t, repository.FeedProfile{Owned: true, Subathon: feed.SubathonRules{FollowSeconds: 10, Tier1Seconds: 300}}, after)
}

func TestAddStarterCreatesACustomOverlay(t *testing.T) {
	repo, _ := setupStore(t)

	created, token, err := repo.AddStarter(t.Context(), userID, "alerts")
	_, _, unknown := repo.AddStarter(t.Context(), userID, "nope")

	require.NoError(t, err)
	require.Len(t, token, 43)
	require.Equal(t, [2]string{string(overlays.KindCustom), "Alerts"}, [2]string{created.Kind, created.Name})
	require.ErrorIs(t, unknown, repository.ErrUnknownTemplate)
	require.True(t, repository.IsStarter("alerts"))
	require.False(t, repository.IsStarter(string(overlays.TemplatePowered)))
}

func TestSaveConflictReturnsTheStoredOverlay(t *testing.T) {
	repo, objects := setupStore(t)
	mine := seedAsset(t, repo, objects, owned(userID, "aaaa"))
	created, _, err := repo.CreateCustom(t.Context(), userID, customDoc(mine.ID))
	require.NoError(t, err)

	current, err := repo.SaveCustom(t.Context(), userID, repository.CustomSave{ID: created.ID, Revision: created.Revision + 5, Document: mustMarshal(t, customDoc(mine.ID))})

	require.ErrorIs(t, err, repository.ErrRevisionConflict)
	require.Equal(t, uint64(userID), current.UserID)
	require.JSONEq(t, string(created.Document), string(current.Document))
}
