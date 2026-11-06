// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cooldownChannel = uint64(5)
	claimers        = 100
)

func eachCooldownStore(t *testing.T, run func(t *testing.T, repo *repository.RecipeState, raw *sql.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		repo, raw := sqliteStateRepo(t)
		run(t, repo, raw)
	})
	t.Run("mysql", func(t *testing.T) {
		repo, raw := mysqlStateRepo(t)
		raw.SetMaxOpenConns(8)
		run(t, repo, raw)
	})
}

func window(key string) repository.CooldownWindow {
	return repository.CooldownWindow{Key: key, TTL: 7 * 24 * time.Hour}
}

func claimCooldowns(t *testing.T, repo *repository.RecipeState, windows ...repository.CooldownWindow) ([]repository.CooldownHold, bool) {
	t.Helper()
	holds, won, err := repo.CooldownClaimAll(context.Background(), repository.CooldownClaim{UserID: cooldownChannel, Windows: windows})
	require.NoError(t, err)
	return holds, won
}

func expireCooldown(t *testing.T, raw *sql.DB, key string) {
	t.Helper()
	_, err := raw.Exec("UPDATE cooldown_claims SET expires_at_ms = ? WHERE cooldown_key = ?", time.Now().Add(-time.Second).UnixMilli(), key)
	require.NoError(t, err)
}

func cooldownRows(t *testing.T, raw *sql.DB) []string {
	t.Helper()
	rows, err := raw.Query("SELECT cooldown_key FROM cooldown_claims ORDER BY cooldown_key")
	require.NoError(t, err)
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		require.NoError(t, rows.Scan(&key))
		keys = append(keys, key)
	}
	require.NoError(t, rows.Err())
	return keys
}

func TestCooldownWindowRefusesUntilExpiry(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		holds, won := claimCooldowns(t, repo, window("k"))
		require.True(t, won)
		require.Len(t, holds, 1)
		assert.InDelta(t, time.Now().Add(7*24*time.Hour).UnixMilli(), holds[0].ExpiresAtMS, 60_000)

		_, won = claimCooldowns(t, repo, window("k"))
		assert.False(t, won, "a live window refuses")

		ctx := context.Background()
		_, won, err := repo.CooldownClaimAll(ctx, repository.CooldownClaim{UserID: cooldownChannel + 1, Windows: []repository.CooldownWindow{window("k")}})
		require.NoError(t, err)
		assert.True(t, won, "the same key in another channel is free")

		expireCooldown(t, raw, "k")
		again, won := claimCooldowns(t, repo, window("k"))
		assert.True(t, won, "an expired window is free")
		assert.Greater(t, again[0].ExpiresAtMS, time.Now().UnixMilli())
	})
}

func oneWinnerAmong(t *testing.T, repo *repository.RecipeState, windows func(i int) []repository.CooldownWindow) int64 {
	t.Helper()
	var winners atomic.Int64
	errs := concurrently(claimers, func(ctx context.Context, i int) error {
		_, won, err := repo.CooldownClaimAll(ctx, repository.CooldownClaim{UserID: cooldownChannel, Windows: windows(i)})
		if won {
			winners.Add(1)
		}
		return err
	})
	for _, err := range errs {
		require.NoError(t, err)
	}
	return winners.Load()
}

func TestCooldownConcurrentClaimersHaveOneWinner(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		same := func(int) []repository.CooldownWindow { return []repository.CooldownWindow{window("race")} }

		assert.EqualValues(t, 1, oneWinnerAmong(t, repo, same), "absent row")
		assert.EqualValues(t, 0, oneWinnerAmong(t, repo, same), "live row")

		expireCooldown(t, raw, "race")
		assert.EqualValues(t, 1, oneWinnerAmong(t, repo, same), "expired row")
		assert.Equal(t, []string{"race"}, cooldownRows(t, raw))
	})
}

func TestCooldownMixedWindowsOneWinnerAcrossViewers(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		winners := oneWinnerAmong(t, repo, func(i int) []repository.CooldownWindow {
			return []repository.CooldownWindow{window(fmt.Sprintf("viewer:%d", i)), window("global")}
		})

		assert.EqualValues(t, 1, winners)
		rows := cooldownRows(t, raw)
		assert.Len(t, rows, 2, "every refused viewer gave its row back: %v", rows)
		assert.Contains(t, rows, "global")
	})
}

func TestCooldownClaimUnderCleanupKeepsOneWinner(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		ctx, stop := context.WithCancel(context.Background())
		cleaned := make(chan struct{})
		go func() {
			defer close(cleaned)
			for ctx.Err() == nil {
				_ = repo.PruneExpiredCooldowns(ctx)
			}
		}()
		defer func() { stop(); <-cleaned }()

		same := func(int) []repository.CooldownWindow { return []repository.CooldownWindow{window("swept")} }
		for range 5 {
			assert.EqualValues(t, 1, oneWinnerAmong(t, repo, same))
			expireCooldown(t, raw, "swept")
		}
	})
}

func TestCooldownRefusalSpendsNothing(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		_, won := claimCooldowns(t, repo, window("taken"))
		require.True(t, won)

		_, won = claimCooldowns(t, repo, window("free"), window("taken"))

		assert.False(t, won)
		assert.Equal(t, []string{"taken"}, cooldownRows(t, raw), "the free window is not spent")
		holds, won := claimCooldowns(t, repo, window("free"))
		assert.True(t, won)
		assert.Len(t, holds, 1)
	})
}

func TestCooldownReleaseGivesBackOnlyItsOwnClaim(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		ctx := context.Background()
		first, won := claimCooldowns(t, repo, window("k"))
		require.True(t, won)
		expireCooldown(t, raw, "k")
		second, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "k", TTL: 8 * 24 * time.Hour})
		require.True(t, won)

		require.NoError(t, repo.CooldownRelease(ctx, cooldownChannel, first))
		_, won = claimCooldowns(t, repo, window("k"))
		assert.False(t, won, "a stale release leaves the later winner standing")

		require.NoError(t, repo.CooldownRelease(ctx, cooldownChannel, second))
		_, won = claimCooldowns(t, repo, window("k"))
		assert.True(t, won)
	})
}

func TestCooldownPruneDeletesOnlyExpiredPastOneBatch(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		ctx := context.Background()
		const expired = 2500
		past := time.Now().Add(-time.Hour).UnixMilli()
		values := make([]string, expired)
		for i := range values {
			values[i] = fmt.Sprintf("(%d, 'old:%d', %d)", i+1, i, past)
		}
		_, err := raw.Exec("INSERT INTO cooldown_claims (user_id, cooldown_key, expires_at_ms) VALUES " + strings.Join(values, ","))
		require.NoError(t, err)
		_, won := claimCooldowns(t, repo, window("live"))
		require.True(t, won)

		require.NoError(t, repo.PruneExpiredCooldowns(ctx))

		assert.Equal(t, []string{"live"}, cooldownRows(t, raw))
	})
}

func TestCooldownRefusalNamesTheWindowAndItsExpiry(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		holds, won := claimCooldowns(t, repo, window("taken"))
		require.True(t, won)

		refused, won := claimCooldowns(t, repo, window("free"), window("taken"))

		assert.False(t, won)
		assert.Equal(t, holds, refused)
		assert.Equal(t, []string{"taken"}, cooldownRows(t, raw))
	})
}

func TestCooldownClaimRefusesBadInput(t *testing.T) {
	repo, _ := sqliteStateRepo(t)
	ctx := context.Background()
	for name, claim := range map[string]repository.CooldownClaim{
		"no channel":  {Windows: []repository.CooldownWindow{window("k")}},
		"no windows":  {UserID: cooldownChannel},
		"three":       {UserID: cooldownChannel, Windows: []repository.CooldownWindow{window("a"), window("b"), window("c")}},
		"empty key":   {UserID: cooldownChannel, Windows: []repository.CooldownWindow{window("")}},
		"long key":    {UserID: cooldownChannel, Windows: []repository.CooldownWindow{window(strings.Repeat("k", 129))}},
		"no ttl":      {UserID: cooldownChannel, Windows: []repository.CooldownWindow{{Key: "k"}}},
		"over thirty": {UserID: cooldownChannel, Windows: []repository.CooldownWindow{{Key: "k", TTL: 30*24*time.Hour + time.Second}}},
	} {
		_, won, err := repo.CooldownClaimAll(ctx, claim)
		assert.False(t, won, name)
		assert.ErrorIs(t, err, repository.ErrInvalidInput, name)
	}
}

func TestCooldownMySQLClusteredOnNaturalKey(t *testing.T) {
	_, raw := mysqlStateRepo(t)
	rows, err := raw.Query("SELECT COLUMN_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'cooldown_claims' AND INDEX_NAME = 'PRIMARY' ORDER BY SEQ_IN_INDEX")
	require.NoError(t, err)
	defer rows.Close()
	columns := []string{}
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		columns = append(columns, column)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"user_id", "cooldown_key"}, columns)
}
