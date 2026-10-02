// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileSongQueueRetiresSkippedRequestsAndRanksSurvivors(t *testing.T) {
	now := time.Now()
	doc := songQueueDoc{Up: []SongEntry{
		{TrackID: "a", EnqueuedAt: now.UnixMilli()},
		{TrackID: "b", EnqueuedAt: now.UnixMilli()},
		{TrackID: "c", EnqueuedAt: now.UnixMilli()},
	}}
	assert.True(t, reconcileSongQueue(&doc, PlayerQueueIDs{CurrentID: "external", UpcomingIDs: []string{"b", "c"}}, now))
	require.Len(t, doc.Up, 2)
	assert.Equal(t, "b", doc.Up[0].TrackID)
	assert.Equal(t, "c", doc.Up[1].TrackID)
}

func TestReconcileSongQueueRetiresLastSkippedRequestAfterGrace(t *testing.T) {
	now := time.Now()
	doc := songQueueDoc{Current: &SongEntry{TrackID: "played"}, Up: []SongEntry{{TrackID: "skipped", EnqueuedAt: now.Add(-time.Minute).UnixMilli()}}}
	assert.True(t, reconcileSongQueue(&doc, PlayerQueueIDs{CurrentID: "external"}, now))
	assert.Nil(t, doc.Current)
	assert.Empty(t, doc.Up)
}

func TestReconcileSongQueueKeepsFreshAndBeyondVisibleWindow(t *testing.T) {
	now := time.Now()
	doc := songQueueDoc{Up: []SongEntry{
		{TrackID: "fresh", EnqueuedAt: now.UnixMilli()},
		{TrackID: "old", EnqueuedAt: now.Add(-time.Minute).UnixMilli()},
	}}
	assert.True(t, reconcileSongQueue(&doc, PlayerQueueIDs{CurrentID: "external"}, now))
	require.Len(t, doc.Up, 1)
	assert.Equal(t, "fresh", doc.Up[0].TrackID)

	visible := make([]string, 20)
	for i := range visible {
		visible[i] = "other"
	}
	doc = songQueueDoc{Up: []SongEntry{{TrackID: "beyond", EnqueuedAt: now.Add(-time.Minute).UnixMilli()}}}
	assert.False(t, reconcileSongQueue(&doc, PlayerQueueIDs{CurrentID: "external", UpcomingIDs: visible}, now))
	assert.Len(t, doc.Up, 1)
}

func TestReconcileSongQueuePromotesActualCurrentAcrossMissedTracks(t *testing.T) {
	now := time.Now()
	doc := songQueueDoc{Current: &SongEntry{TrackID: "old"}, Up: []SongEntry{
		{TrackID: "skipped"}, {TrackID: "actual"}, {TrackID: "later"},
	}}
	assert.True(t, reconcileSongQueue(&doc, PlayerQueueIDs{CurrentID: "actual", UpcomingIDs: []string{"later"}}, now))
	require.NotNil(t, doc.Current)
	assert.Equal(t, "actual", doc.Current.TrackID)
	require.Len(t, doc.Up, 1)
	assert.Equal(t, "later", doc.Up[0].TrackID)
}

func TestReconcileSongQueueKeepsNewestDuplicate(t *testing.T) {
	now := time.Now()
	doc := songQueueDoc{Up: []SongEntry{
		{TrackID: "same", RequesterID: "first", EnqueuedAt: now.Add(-time.Minute).UnixMilli()},
		{TrackID: "same", RequesterID: "second", EnqueuedAt: now.Add(-time.Minute).UnixMilli()},
	}}
	assert.True(t, reconcileSongQueue(&doc, PlayerQueueIDs{CurrentID: "external", UpcomingIDs: []string{"same"}}, now))
	require.Len(t, doc.Up, 1)
	assert.Equal(t, "second", doc.Up[0].RequesterID)
}

func TestReconcileSongQueueLeavesIdlePlayerAlone(t *testing.T) {
	now := time.Now()
	doc := songQueueDoc{Current: &SongEntry{TrackID: "paused"}, Up: []SongEntry{{TrackID: "waiting", EnqueuedAt: now.Add(-time.Hour).UnixMilli()}}}
	assert.False(t, reconcileSongQueue(&doc, PlayerQueueIDs{}, now))
	assert.Equal(t, "paused", doc.Current.TrackID)
	assert.Len(t, doc.Up, 1)
}
