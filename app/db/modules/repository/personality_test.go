// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync/atomic"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"

	"ItsBagelBot/internal/testdb"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPersonality(t *testing.T, legacy ...int64) (*ent.Client, *repository.Personality) {
	t.Helper()

	client := testdb.Open(t, "modpersonalityent", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	if len(legacy) > 0 {
		require.NoError(t, client.FeedCounter.Create().SetID(1).SetCount(legacy[0]).Exec(t.Context()))
	}
	require.NoError(t, repository.MigrateSchema(t.Context(), client, testdb.Driver))

	return client, repository.NewPersonality(client)
}

type feeder struct {
	id   uint64
	name string
}

func feed(t *testing.T, repo *repository.Personality, who feeder, times int) repository.FeedTotals {
	t.Helper()
	var totals repository.FeedTotals
	for range times {
		var err error
		totals, err = repo.FeedBump(context.Background(), who.id, who.name, uuid.NewString())
		require.NoError(t, err)
	}
	return totals
}

func TestFeedBumpCreatesThenCounts(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	for want := int64(1); want <= 3; want++ {
		totals, err := repo.FeedBump(ctx, 77, "Crumb", fmt.Sprint(want))
		require.NoError(t, err)
		assert.Equal(t, want, totals.Total)
		assert.Equal(t, want, totals.Channel)
		assert.Equal(t, uint64(1), totals.Rank, "the only channel feeding is the one on top")
	}
}

func TestFeedBumpIncrementsExistingRow(t *testing.T) {
	_, repo := setupPersonality(t, 41)
	ctx := context.Background()

	totals, err := repo.FeedBump(ctx, 0, "", "global-only")
	require.NoError(t, err)
	assert.Equal(t, int64(42), totals.Total, "bump must ride the existing permanent row")
	assert.Zero(t, totals.Channel, "a feeding with no broadcaster writes no channel row")
	assert.Zero(t, totals.Rank)
}

func TestFeedBumpSplitsFleetTotalFromChannelCounts(t *testing.T) {
	_, repo := setupPersonality(t)

	feed(t, repo, feeder{10, "Ten"}, 3)
	totals := feed(t, repo, feeder{20, "Twenty"}, 1)

	assert.Equal(t, int64(4), totals.Total, "one bagel, fed by every channel")
	assert.Equal(t, int64(1), totals.Channel)
	assert.Equal(t, uint64(2), totals.Rank, "one channel has fed more")
}

func TestFeedBumpTracksNameWithoutErasingIt(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	feed(t, repo, feeder{10, "Old"}, 1)
	feed(t, repo, feeder{10, "New"}, 1)
	feed(t, repo, feeder{10, ""}, 1)

	board, err := repo.FeedBoard(ctx, 0)
	require.NoError(t, err)
	require.Len(t, board, 1)
	assert.Equal(t, "New", board[0].Name)
	assert.Equal(t, int64(3), board[0].Count)
}

func TestFeedBoardRanksHighestFirstAndHonoursLimit(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	for id, times := range map[uint64]int{10: 5, 20: 9, 30: 1} {
		feed(t, repo, feeder{id, ""}, times)
	}

	board, err := repo.FeedBoard(ctx, 2)
	require.NoError(t, err)
	require.Len(t, board, 2, "the limit caps the board")
	assert.Equal(t, []repository.FeedBoardRow{{BroadcasterID: 20, Count: 9}, {BroadcasterID: 10, Count: 5}}, board)

	full, err := repo.FeedBoard(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, full, 3, "no limit returns every ranked channel")

	ranked, err := repo.FeedRanked(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), ranked)
}

func TestFeedChannelReadsStandingWithoutBumping(t *testing.T) {
	_, repo := setupPersonality(t)
	ctx := context.Background()

	feed(t, repo, feeder{10, "Ten"}, 2)
	feed(t, repo, feeder{20, "Twenty"}, 1)

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

func TestFeedBumpRollsBackWhenChannelIsAtLimit(t *testing.T) {
	client, repo := setupPersonality(t, 10)
	ctx := context.Background()
	require.NoError(t, client.ChannelFeedCounter.Create().SetID(77).SetCount(data.MaxCounter).Exec(ctx))

	_, err := repo.FeedBump(ctx, 77, "Seventy Seven", "channel-limit")
	require.True(t, errors.Is(err, repository.ErrFeedCountLimit), "unexpected error: %v", err)
	total, err := repo.FeedTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(10), total)
	channel, _, err := repo.FeedChannel(ctx, 77)
	require.NoError(t, err)
	assert.Equal(t, data.MaxCounter, channel)
}

func TestFeedBumpRefusesGlobalLimit(t *testing.T) {
	client, repo := setupPersonality(t, data.MaxCounter)
	ctx := context.Background()
	_, err := repo.FeedBump(ctx, 77, "Seventy Seven", "global-limit")
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
		"INSERT INTO feed_counters (id, count) VALUES (258, ?)",
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
	require.Equal(t, [2]int64{first.Total, first.Channel}, [2]int64{replay.Total, replay.Channel})
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

func TestFeedMigrationPreservesTotalsAndReceipts(t *testing.T) {
	for _, initial := range []int64{0, 41, data.MaxCounter - 1} {
		t.Run(fmt.Sprint(initial), func(t *testing.T) {
			client, repo := setupPersonality(t, initial)
			ctx := t.Context()
			total, err := repo.FeedTotal(ctx)
			require.NoError(t, err)
			require.Equal(t, initial, total)

			first, err := repo.FeedBump(ctx, 77, "Crumb", "migration-feed")
			require.NoError(t, err)
			base, err := client.FeedCounter.Get(ctx, 1)
			require.NoError(t, err)
			assert.Equal(t, initial, base.Count)
			require.NoError(t, repository.MigrateSchema(ctx, client, testdb.Driver))
			replay, err := repo.FeedBump(ctx, 77, "Crumb", "migration-feed")
			require.NoError(t, err)
			require.Equal(t, [2]int64{initial + 1, 1}, [2]int64{first.Total, first.Channel})
			assert.Equal(t, [2]int64{first.Total, first.Channel}, [2]int64{replay.Total, replay.Channel})
		})
	}
}

func TestFeedRedistributesAFullSlot(t *testing.T) {
	client, repo := setupPersonality(t)
	ctx := t.Context()
	identity := "full-slot"
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(identity))
	rowID := int(hash.Sum64()%256) + 2
	initial := data.MaxCounter / 256
	require.NoError(t, client.FeedCounter.UpdateOneID(rowID).SetCount(initial).Exec(ctx))
	result, err := repo.FeedBump(ctx, 77, "Crumb", identity)
	require.NoError(t, err)
	assert.Equal(t, initial+1, result.Total)

	total, err := repo.FeedTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, result.Total, total)
}

func TestFeedBumpRejectsInvalidEventIdentity(t *testing.T) {
	for _, ids := range [][]string{nil, {""}, {strings.Repeat("x", 256)}} {
		t.Run(fmt.Sprintf("identity-%d", len(strings.Join(ids, ""))), func(t *testing.T) {
			client, repo := setupPersonality(t)
			_, err := repo.FeedBump(t.Context(), 77, "Crumb", ids...)
			require.ErrorIs(t, err, repository.ErrInvalidInput)
			receipts, err := client.FeedReceipt.Query().Count(t.Context())
			require.NoError(t, err)
			assert.Zero(t, receipts)
			total, err := repo.FeedTotal(t.Context())
			require.NoError(t, err)
			assert.Zero(t, total)
		})
	}
}

func TestFeedReplayFinishesAfterPostCommitFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject func(*ent.Client, error)
	}{
		{"read", failFeedTotalRead},
		{"receipt", failFeedReceiptUpdate},
	} {
		t.Run(tc.name, func(t *testing.T) { testFeedPostCommitFailure(t, tc.inject) })
	}
}

func failFeedTotalRead(client *ent.Client, failure error) {
	var reads atomic.Int32
	client.FeedCounter.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if reads.Add(1) == 2 {
				return nil, failure
			}
			return next.Query(ctx, query)
		})
	}))
}

func failFeedReceiptUpdate(client *ent.Client, failure error) {
	var fail atomic.Bool
	fail.Store(true)
	client.FeedReceipt.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op() != ent.OpUpdate {
				return next.Mutate(ctx, mutation)
			}
			_, finishing := mutation.Field("total")
			if finishing && fail.CompareAndSwap(true, false) {
				return nil, failure
			}
			return next.Mutate(ctx, mutation)
		})
	})
}

func testFeedPostCommitFailure(t *testing.T, inject func(*ent.Client, error)) {
	t.Helper()
	client, repo := setupPersonality(t)
	failure := errors.New("finish unavailable")
	inject(client, failure)
	var ranks atomic.Int32
	client.ChannelFeedCounter.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			ranks.Add(1)
			return next.Query(ctx, query)
		})
	}))
	ctx := t.Context()
	result, err := repo.FeedBump(ctx, 77, "Crumb", "interrupted")
	require.ErrorIs(t, err, failure)
	assert.Equal(t, repository.FeedTotals{}, result)
	assert.Zero(t, ranks.Load())
	receipt, err := client.FeedReceipt.Get(ctx, "interrupted")
	require.NoError(t, err)
	require.Equal(t, [2]int64{0, 1}, [2]int64{receipt.Total, receipt.Channel})

	_, err = repo.FeedBump(ctx, 78, "Other", "later")
	require.NoError(t, err)
	result, err = repo.FeedBump(ctx, 77, "Crumb", "interrupted")
	require.NoError(t, err)
	assert.Equal(t, [2]int64{2, 1}, [2]int64{result.Total, result.Channel})

	total, err := repo.FeedTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
}

func TestFeedMigrationPreservesLegacyBaseAndExistingSlots(t *testing.T) {
	client, repo := setupPersonality(t, 41)
	ctx := t.Context()
	require.NoError(t, client.FeedCounter.UpdateOneID(2).SetCount(3).Exec(ctx))
	require.NoError(t, client.FeedCounter.UpdateOneID(257).SetCount(5).Exec(ctx))
	require.NoError(t, client.FeedCounter.DeleteOneID(3).Exec(ctx))
	require.NoError(t, repository.MigrateSchema(ctx, client, testdb.Driver))
	require.NoError(t, client.FeedCounter.UpdateOneID(1).AddCount(7).Exec(ctx))
	require.NoError(t, repository.MigrateSchema(ctx, client, testdb.Driver))
	for id, want := range map[int]int64{1: 48, 2: 3, 3: 0, 257: 5} {
		row, err := client.FeedCounter.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, want, row.Count)
	}
	result, err := repo.FeedBump(ctx, 77, "Crumb", "after-legacy")
	require.NoError(t, err)
	assert.Equal(t, int64(57), result.Total)
	base, err := client.FeedCounter.Get(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(48), base.Count)
}
