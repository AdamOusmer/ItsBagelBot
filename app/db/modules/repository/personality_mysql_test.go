// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mysqlPersonality(t *testing.T, initial ...int64) (*ent.Client, *sql.DB) {
	t.Helper()
	raw := testdb.MySQL(t, "modules_feed_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	ctx := t.Context()
	total := int64(41)
	if len(initial) > 0 {
		total = initial[0]
	}
	require.NoError(t, client.Schema.Create(ctx))
	require.NoError(t, client.FeedCounter.Create().SetID(1).SetCount(total).Exec(ctx))
	require.NoError(t, client.FeedReceipt.Create().SetID("legacy").SetTotal(41).SetChannel(7).Exec(ctx))
	require.NoError(t, repository.MigrateSchema(ctx, client, "mysql"))
	return client, raw
}

func TestFeedMySQLConcurrentIntegerLimit(t *testing.T) {
	client, raw := mysqlPersonality(t, data.MaxCounter-16)
	repo := repository.NewPersonality(client)
	before := feedDeadlocks(t, raw)
	errs := concurrently(64, func(ctx context.Context, i int) error {
		_, err := repo.FeedBump(ctx, uint64(i+1), "Crumb", fmt.Sprintf("limit-%d", i))
		return err
	})
	var committed int
	for _, err := range errs {
		if err == nil {
			committed++
			continue
		}
		require.ErrorIs(t, err, repository.ErrFeedCountLimit)
	}
	assert.Equal(t, 16, committed)
	assert.Equal(t, before, feedDeadlocks(t, raw))

	total, err := repo.FeedTotal(t.Context())
	require.NoError(t, err)
	assert.Equal(t, data.MaxCounter, total)
	receipts, err := client.FeedReceipt.Query().Count(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 17, receipts)
}

func feedDeadlocks(t *testing.T, raw *sql.DB) int64 {
	t.Helper()
	var count int64
	err := raw.QueryRow("SELECT `COUNT` FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'").Scan(&count)
	require.NoError(t, err)
	return count
}

func TestFeedMySQLConcurrentChannelsAndReplays(t *testing.T) {
	for _, duplicates := range []int{1, 2} {
		t.Run(fmt.Sprintf("copies-%d", duplicates), func(t *testing.T) {
			testConcurrentFeedCopies(t, duplicates)
		})
	}
}

func testConcurrentFeedCopies(t *testing.T, copies int) {
	t.Helper()
	client, raw := mysqlPersonality(t)
	repos := []*repository.Personality{repository.NewPersonality(client), repository.NewPersonality(ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw))))}
	before := feedDeadlocks(t, raw)
	const channels, events = 64, 12
	errs := concurrently(channels*copies, func(ctx context.Context, worker int) error {
		channel := worker % channels
		return feedMySQLChannel(ctx, repos[channel%len(repos)], channel, events)
	})
	for _, err := range errs {
		require.NoError(t, err)
	}
	ctx := t.Context()
	total, err := repos[0].FeedTotal(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(41+channels*events), total)
	assert.Equal(t, before, feedDeadlocks(t, raw), "server must report no new deadlocks")

	board, err := repos[0].FeedBoard(ctx, 0)
	require.NoError(t, err)
	require.Len(t, board, channels)
	for _, row := range board {
		assert.Equal(t, int64(events), row.Count)
	}
	receipts, err := client.FeedReceipt.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, receipts, 1+channels*events)
	for _, receipt := range receipts {
		assert.Positive(t, receipt.Total)
	}
	require.NoError(t, repository.MigrateSchema(ctx, client, "mysql"))
	legacy, err := repos[0].FeedBump(ctx, 77, "Crumb", "legacy")
	require.NoError(t, err)
	assert.Equal(t, [2]int64{41, 7}, [2]int64{legacy.Total, legacy.Channel})
}

func feedMySQLChannel(ctx context.Context, repo *repository.Personality, channel, events int) error {
	for event := range events {
		id := fmt.Sprintf("channel-%d-event-%d", channel, event)
		first, err := repo.FeedBump(ctx, uint64(channel+1), "Crumb", id)
		if err != nil {
			return err
		}
		replay, err := repo.FeedBump(ctx, uint64(channel+1), "Crumb", id)
		if err != nil {
			return err
		}
		if first.Total != replay.Total || first.Channel != replay.Channel {
			return fmt.Errorf("receipt %s changed: first=%+v replay=%+v", id, first, replay)
		}
	}
	return nil
}

func TestFeedMySQLDoesNotWaitForLegacyRow(t *testing.T) {
	client, raw := mysqlPersonality(t)
	ctx := t.Context()
	tx, err := raw.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, "UPDATE feed_counters SET count = count + 100 WHERE id = 1")
	require.NoError(t, err)
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err := repository.NewPersonality(client).FeedBump(deadline, 77, "Crumb", "independent-channel")
	require.NoError(t, err)
	assert.Equal(t, int64(42), result.Total)
}

func TestFeedMySQLMigrationDoesNotLockExistingRows(t *testing.T) {
	for _, id := range []int{1, 2, 257} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			client, raw := mysqlPersonality(t)
			ctx := t.Context()
			require.NoError(t, client.FeedCounter.DeleteOneID(3).Exec(ctx))
			tx, err := raw.BeginTx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback() })
			_, err = tx.ExecContext(ctx, "UPDATE feed_counters SET count = count + 100 WHERE id = ?", id)
			require.NoError(t, err)
			deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			require.NoError(t, repository.MigrateSchema(deadline, client, "mysql"))
			require.NoError(t, tx.Commit())
			base, err := client.FeedCounter.Get(ctx, 1)
			require.NoError(t, err)
			want := int64(41)
			if id == 1 {
				want += 100
			}
			assert.Equal(t, want, base.Count)
			total, err := repository.NewPersonality(client).FeedTotal(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(141), total)
		})
	}
}

func TestFeedMySQLRetriesTheWholeTransaction(t *testing.T) {
	for _, code := range []uint16{db.MySQLDeadlock, db.MySQLLockWaitTimeout} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client, _ := mysqlPersonality(t)
			var attempts atomic.Int32
			client.ChannelFeedCounter.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					if attempts.Add(1) == 1 {
						return nil, &mysql.MySQLError{Number: code, Message: "retry transaction"}
					}
					return next.Mutate(ctx, mutation)
				})
			})
			repo := repository.NewPersonality(client)
			result, err := repo.FeedBump(t.Context(), 77, "Crumb", "retried")
			require.NoError(t, err)
			assert.Equal(t, [2]int64{42, 1}, [2]int64{result.Total, result.Channel})

			total, err := repo.FeedTotal(t.Context())
			require.NoError(t, err)
			assert.Equal(t, int64(42), total)
		})
	}
}
