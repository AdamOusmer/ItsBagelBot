// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extendReq(key string, addSeconds int64) repository.CooldownExtendRequest {
	return repository.CooldownExtendRequest{UserID: cooldownChannel, Key: key, AddSeconds: addSeconds}
}

func TestCooldownExtendOnARunningWindow(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, _ *sql.DB) {
		ctx := context.Background()
		_, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "named:k", TTL: time.Hour})
		require.True(t, won)

		expiry, found, capped, err := repo.CooldownExtend(ctx, extendReq("named:k", 30))
		require.NoError(t, err)
		assert.True(t, found)
		assert.False(t, capped)
		assert.InDelta(t, time.Now().Add(time.Hour+30*time.Second).UnixMilli(), expiry, 60_000)
	})
}

func TestCooldownExtendLeavesAnAbsentOrExpiredRowUntouched(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		ctx := context.Background()
		_, found, capped, err := repo.CooldownExtend(ctx, extendReq("named:missing", 30))
		require.NoError(t, err)
		assert.False(t, found)
		assert.False(t, capped)

		_, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "named:expired", TTL: time.Hour})
		require.True(t, won)
		expireCooldown(t, raw, "named:expired")

		_, found, _, err = repo.CooldownExtend(ctx, extendReq("named:expired", 30))
		require.NoError(t, err)
		assert.False(t, found, "an expired row is not running")
	})
}

func TestCooldownExtendClampsAtTheSystemCeiling(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, _ *sql.DB) {
		ctx := context.Background()
		_, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "named:cap", TTL: time.Hour})
		require.True(t, won)

		expiry, found, capped, err := repo.CooldownExtend(ctx, extendReq("named:cap", int64(automation.MaxCooldownSeconds)))
		require.NoError(t, err)
		assert.True(t, found)
		assert.True(t, capped, "the uncapped target overshoots MaxCooldownSeconds from now")
		assert.InDelta(t, time.Now().Add(time.Duration(automation.MaxCooldownSeconds)*time.Second).UnixMilli(), expiry, 60_000)

		again, found, capped, err := repo.CooldownExtend(ctx, extendReq("named:cap", 30))
		require.NoError(t, err)
		assert.True(t, found)
		assert.True(t, capped)
		assert.GreaterOrEqual(t, again, expiry, "a capped extend never shrinks the window")
	})
}

// A window already well past a day, far short of the system ceiling, still adds exactly: GREATEST only ever
// guards against LEAST's cap landing below the window's current expiry, never against a plain add.
func TestCooldownExtendOnAFarFutureWindowStillAddsExactly(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		ctx := context.Background()
		_, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "named:far", TTL: time.Hour})
		require.True(t, won)
		far := time.Now().Add(72 * time.Hour).UnixMilli()
		_, err := raw.Exec("UPDATE cooldown_claims SET expires_at_ms = ? WHERE cooldown_key = ?", far, "named:far")
		require.NoError(t, err)

		expiry, found, capped, err := repo.CooldownExtend(ctx, extendReq("named:far", 1))
		require.NoError(t, err)
		assert.True(t, found)
		assert.False(t, capped)
		assert.Equal(t, far+1000, expiry)
	})
}

// MySQL's client reports zero rows affected when an UPDATE's SET clause computes the value already stored, even
// though the WHERE clause matched a live row. This proves that premise directly against the real driver, so the
// next test's claim that CooldownExtend no longer trusts RowsAffected rests on more than a guess.
func TestMySQLReportsZeroRowsAffectedOnANoopUpdate(t *testing.T) {
	_, raw := mysqlStateRepo(t)
	_, err := raw.Exec("INSERT INTO cooldown_claims (user_id, cooldown_key, expires_at_ms) VALUES (?, ?, ?)", cooldownChannel, "noop", 999)
	require.NoError(t, err)

	res, err := raw.Exec("UPDATE cooldown_claims SET expires_at_ms = 999 WHERE user_id = ? AND cooldown_key = ?", cooldownChannel, "noop")
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	assert.Zero(t, affected, "the matched row's value did not change")
}

// Reproduces the bug by forcing the same no-op update CooldownExtend's own query can hit: a window already past
// where any extend this call could reach leaves the stored value unchanged, so RowsAffected would read 0 on MySQL
// even though the window is very much still running. found must come from reading the row back, not from that count.
func TestCooldownExtendOnARowTheWriteDoesNotChangeIsStillFound(t *testing.T) {
	eachCooldownStore(t, func(t *testing.T, repo *repository.RecipeState, raw *sql.DB) {
		ctx := context.Background()
		_, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "named:noop", TTL: time.Hour})
		require.True(t, won)
		beyondAnyCap := time.Now().Add(2 * time.Duration(automation.MaxCooldownSeconds) * time.Second).UnixMilli()
		_, err := raw.Exec("UPDATE cooldown_claims SET expires_at_ms = ? WHERE cooldown_key = ?", beyondAnyCap, "named:noop")
		require.NoError(t, err)

		expiry, found, _, err := repo.CooldownExtend(ctx, extendReq("named:noop", 30))
		require.NoError(t, err)
		assert.True(t, found, "the window is running even though the write changed nothing")
		assert.Equal(t, beyondAnyCap, expiry, "GREATEST kept the larger, already-stored value")
	})
}

func TestCooldownExtendRefusesBadInput(t *testing.T) {
	repo, _ := sqliteStateRepo(t)
	ctx := context.Background()
	_, _, _, err := repo.CooldownExtend(ctx, repository.CooldownExtendRequest{Key: "k", AddSeconds: 1})
	assert.ErrorIs(t, err, repository.ErrInvalidInput, "no channel")
	_, _, _, err = repo.CooldownExtend(ctx, extendReq("", 1))
	assert.ErrorIs(t, err, repository.ErrInvalidInput, "no key")
	_, _, _, err = repo.CooldownExtend(ctx, extendReq("k", 0))
	assert.ErrorIs(t, err, repository.ErrInvalidInput, "no seconds")
	_, _, _, err = repo.CooldownExtend(ctx, extendReq("k", -1))
	assert.ErrorIs(t, err, repository.ErrInvalidInput, "negative seconds")
}

// TestCooldownConcurrentExtendsSumExactly proves the atomic SQL expression serializes concurrent writers on MySQL
// instead of losing updates, the way a read-then-write round trip would.
func TestCooldownConcurrentExtendsSumExactly(t *testing.T) {
	repo, raw := mysqlStateRepo(t)
	_, won := claimCooldowns(t, repo, repository.CooldownWindow{Key: "race", TTL: time.Hour})
	require.True(t, won)
	var before int64
	require.NoError(t, raw.QueryRow("SELECT expires_at_ms FROM cooldown_claims WHERE user_id = ? AND cooldown_key = ?", cooldownChannel, "race").Scan(&before))

	const writers, addEach = 50, int64(2)
	var winners atomic.Int64
	errs := concurrently(writers, func(ctx context.Context, _ int) error {
		_, found, _, err := repo.CooldownExtend(ctx, extendReq("race", addEach))
		if found {
			winners.Add(1)
		}
		return err
	})
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.EqualValues(t, writers, winners.Load())

	var after int64
	require.NoError(t, raw.QueryRow("SELECT expires_at_ms FROM cooldown_claims WHERE user_id = ? AND cooldown_key = ?", cooldownChannel, "race").Scan(&after))
	assert.Equal(t, before+writers*addEach*1000, after, "every writer's addition landed exactly once, none lost")
}
