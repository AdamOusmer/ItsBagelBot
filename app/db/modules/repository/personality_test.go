// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPersonality(t *testing.T) (*ent.Client, *repository.Personality) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:modpersonalityent?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	return client, repository.NewPersonality(client)
}

// The very first feeding must create both the single fleet-wide row and the
// feeding channel's row; every one after that increments them. (True
// concurrency on the first feed is covered by the retry loop and MySQL's
// atomic UPDATE; sqlite serializes writers, so this test keeps to the
// deterministic paths.)
func TestFeedBumpCreatesThenCounts(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	for want := uint64(1); want <= 3; want++ {
		totals, err := repo.FeedBump(ctx, 77, "Crumb")
		require.NoError(t, err)
		assert.Equal(t, want, totals.Total)
		assert.Equal(t, want, totals.Channel)
		assert.Equal(t, uint64(1), totals.Rank, "the only channel feeding is the one on top")
	}
}

func TestFeedBumpIncrementsExistingRow(t *testing.T) {
	client, repo := setupPersonality(t)
	ctx := context.Background()

	require.NoError(t, client.FeedCounter.Create().SetID(1).SetCount(41).Exec(ctx))

	totals, err := repo.FeedBump(ctx, 0, "")
	require.NoError(t, err)
	assert.Equal(t, uint64(42), totals.Total, "bump must ride the existing permanent row")
	assert.Zero(t, totals.Channel, "a feeding with no broadcaster writes no channel row")
	assert.Zero(t, totals.Rank)
}

// The fleet-wide total counts every channel's feedings; each channel row
// counts only its own, and the rank follows the counts.
func TestFeedBumpSplitsFleetTotalFromChannelCounts(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	for range 3 {
		_, err := repo.FeedBump(ctx, 10, "Ten")
		require.NoError(t, err)
	}
	totals, err := repo.FeedBump(ctx, 20, "Twenty")
	require.NoError(t, err)

	assert.Equal(t, uint64(4), totals.Total, "one bagel, fed by every channel")
	assert.Equal(t, uint64(1), totals.Channel)
	assert.Equal(t, uint64(2), totals.Rank, "one channel has fed more")
}

// A rename follows the channel; a feeding that carries no name leaves the
// stored one alone rather than blanking the leaderboard entry.
func TestFeedBumpTracksNameWithoutErasingIt(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	_, err := repo.FeedBump(ctx, 10, "Old")
	require.NoError(t, err)
	_, err = repo.FeedBump(ctx, 10, "New")
	require.NoError(t, err)
	_, err = repo.FeedBump(ctx, 10, "")
	require.NoError(t, err)

	board, err := repo.FeedBoard(ctx, 0)
	require.NoError(t, err)
	require.Len(t, board, 1)
	assert.Equal(t, "New", board[0].Name)
	assert.Equal(t, uint64(3), board[0].Count)
}

func TestFeedBoardRanksHighestFirstAndHonoursLimit(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	feedings := map[uint64]int{10: 5, 20: 9, 30: 1}
	for id, times := range feedings {
		for range times {
			_, err := repo.FeedBump(ctx, id, "")
			require.NoError(t, err)
		}
	}

	board, err := repo.FeedBoard(ctx, 2)
	require.NoError(t, err)
	require.Len(t, board, 2, "the limit caps the board")
	assert.Equal(t, uint64(20), board[0].BroadcasterID)
	assert.Equal(t, uint64(9), board[0].Count)
	assert.Equal(t, uint64(10), board[1].BroadcasterID)

	full, err := repo.FeedBoard(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, full, 3, "no limit returns every ranked channel")

	ranked, err := repo.FeedRanked(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), ranked)
}

// The leaderboard read never feeds the bagel, and an unknown channel reads as
// unranked rather than erroring.
func TestFeedChannelReadsStandingWithoutBumping(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	for range 2 {
		_, err := repo.FeedBump(ctx, 10, "Ten")
		require.NoError(t, err)
	}
	_, err := repo.FeedBump(ctx, 20, "Twenty")
	require.NoError(t, err)

	count, rank, err := repo.FeedChannel(ctx, 20)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), count)
	assert.Equal(t, uint64(2), rank)

	count, rank, err = repo.FeedChannel(ctx, 999)
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Zero(t, rank, "a channel that never fed has no rank")

	after, err := repo.FeedBoard(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), after[0].Count, "reading the board must not bump anything")
}
