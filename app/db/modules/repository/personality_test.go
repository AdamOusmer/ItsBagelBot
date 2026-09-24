// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPersonality(t *testing.T) (*ent.Client, *repository.Personality) {
	t.Helper()

	client := testdb.Open(t, "modpersonalityent", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })

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

	for want := int64(1); want <= 3; want++ {
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
	assert.Equal(t, int64(42), totals.Total, "bump must ride the existing permanent row")
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

	assert.Equal(t, int64(4), totals.Total, "one bagel, fed by every channel")
	assert.Equal(t, int64(1), totals.Channel)
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
	assert.Equal(t, int64(3), board[0].Count)
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
	assert.Equal(t, int64(9), board[0].Count)
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
	assert.Equal(t, int64(1), count)
	assert.Equal(t, uint64(2), rank)

	count, rank, err = repo.FeedChannel(ctx, 999)
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Zero(t, rank, "a channel that never fed has no rank")

	after, err := repo.FeedBoard(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(2), after[0].Count, "reading the board must not bump anything")
}

// A channel at the exact JSON integer limit must not advance the fleet total
// alone. The two writes share a transaction, and both values stay unchanged.
func TestFeedBumpRollsBackWhenChannelIsAtLimit(t *testing.T) {
	client, repo := setupPersonality(t)
	ctx := context.Background()
	require.NoError(t, client.FeedCounter.Create().SetID(1).SetCount(10).Exec(ctx))
	require.NoError(t, client.ChannelFeedCounter.Create().SetID(77).SetCount(data.MaxCounter).Exec(ctx))

	_, err := repo.FeedBump(ctx, 77, "Seventy Seven")
	require.True(t, errors.Is(err, repository.ErrFeedCountLimit), "unexpected error: %v", err)
	total, err := repo.FeedTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(10), total)
	channel, _, err := repo.FeedChannel(ctx, 77)
	require.NoError(t, err)
	assert.Equal(t, data.MaxCounter, channel)
}

func TestFeedBumpRefusesGlobalLimit(t *testing.T) {
	client, repo := setupPersonality(t)
	ctx := context.Background()
	require.NoError(t, client.FeedCounter.Create().SetID(1).SetCount(data.MaxCounter).Exec(ctx))
	_, err := repo.FeedBump(ctx, 77, "Seventy Seven")
	require.True(t, errors.Is(err, repository.ErrFeedCountLimit), "unexpected error: %v", err)
	_, err = client.ChannelFeedCounter.Get(ctx, 77)
	assert.True(t, ent.IsNotFound(err))
}

func TestFeedCountDatabaseRange(t *testing.T) {
	_, _ = setupPersonality(t)
	ctx := context.Background()
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN("modpersonalityent"))
	require.NoError(t, err)
	defer raw.Close()
	for _, query := range []string{
		"INSERT INTO feed_counters (id, count) VALUES (1, ?)",
		"INSERT INTO channel_feed_counters (id, count, name) VALUES (77, ?, 'Seventy Seven')",
	} {
		for _, count := range []int64{-1} {
			_, err := raw.ExecContext(ctx, query, count)
			assert.Error(t, err, "database must reject out-of-range count %d", count)
		}
	}
}

func TestFeedReplayReturnsCommittedTotalsWithoutIncrement(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()
	first, err := repo.FeedBump(ctx, 77, "Crumb", "feeding-one")
	require.NoError(t, err)
	second, err := repo.FeedBump(ctx, 77, "Crumb", "feeding-two")
	require.NoError(t, err)
	require.Equal(t, first.Total+1, second.Total)
	replay, err := repo.FeedBump(ctx, 77, "Crumb", "feeding-one")
	require.NoError(t, err)
	require.Equal(t, first.Total, replay.Total)
	require.Equal(t, first.Channel, replay.Channel)
	total, err := repo.FeedTotal(ctx)
	require.NoError(t, err)
	require.Equal(t, second.Total, total)
}

func TestFailedFeedDoesNotRetainReceipt(t *testing.T) {
	client, repo := setupPersonality(t)
	ctx := context.Background()
	require.NoError(t, client.ChannelFeedCounter.Create().SetID(77).SetCount(data.MaxCounter).Exec(ctx))
	_, err := repo.FeedBump(ctx, 77, "Crumb", "feeding-one")
	require.ErrorIs(t, err, repository.ErrFeedCountLimit)
	_, err = client.FeedReceipt.Get(ctx, "feeding-one")
	require.True(t, ent.IsNotFound(err))
	require.NoError(t, client.ChannelFeedCounter.UpdateOneID(77).SetCount(0).Exec(ctx))
	totals, err := repo.FeedBump(ctx, 77, "Crumb", "feeding-one")
	require.NoError(t, err)
	require.Equal(t, int64(1), totals.Total)
	require.Equal(t, int64(1), totals.Channel)
}
