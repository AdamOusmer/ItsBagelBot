// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository_test

import (
	"ItsBagelBot/app/db/modules/repository"
	"context"
	"database/sql"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCountdownDurableCompletionAndDeduplication(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	ctx := context.Background()
	start, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "Test", Verb: "start", Seconds: 10, OperationID: "start-1"})
	require.NoError(t, err)
	require.True(t, start.Found)
	again, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "test", Verb: "start", Seconds: 10, OperationID: "start-1"})
	require.NoError(t, err)
	require.Equal(t, start, again)
	added, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "TEST", Verb: "add", Seconds: 15, OperationID: "add-1"})
	require.NoError(t, err)
	require.Equal(t, start.ExpiresAtMS+15000, added.ExpiresAtMS)
	again, err = repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "test", Verb: "add", Seconds: 15, OperationID: "add-1"})
	require.NoError(t, err)
	require.Equal(t, added, again)
	_, err = repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "test", Verb: "add", Seconds: 16, OperationID: "add-1"})
	require.Error(t, err)
	other, err := repo.CountdownPeek(ctx, 6, "test")
	require.NoError(t, err)
	require.False(t, other.Found)
	_, err = raw.Exec("UPDATE countdowns SET expires_at_ms=?", time.Now().Add(-time.Second).UnixMilli())
	require.NoError(t, err)
	result, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "test", Verb: "add", Seconds: 1, OperationID: "late"})
	require.NoError(t, err)
	require.False(t, result.Found)
	delivery, found, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.True(t, found)
	_, found, err = repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.False(t, found)
	_, err = repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "test", Verb: "start", Seconds: 10, OperationID: "start-2"})
	require.NoError(t, err)
	wrong := delivery
	wrong.Token = "stale"
	ok, err := repo.CountdownAck(ctx, wrong)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.CountdownAck(ctx, delivery)
	require.NoError(t, err)
	require.True(t, ok)
	active, err := repo.CountdownPeek(ctx, 5, "test")
	require.NoError(t, err)
	require.True(t, active.Found)
}
func TestCountdownLeaseRecovery(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	ctx := context.Background()
	_, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 7, Name: "demo", Verb: "start", Seconds: 1, OperationID: "op"})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE countdowns SET expires_at_ms=?", time.Now().Add(-time.Second).UnixMilli())
	require.NoError(t, err)
	first, found, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.True(t, found)
	_, err = raw.Exec("UPDATE countdown_deliveries SET lease_until_ms=0")
	require.NoError(t, err)
	next, found, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first.Generation, next.Generation)
	require.NotEqual(t, first.Token, next.Token)
	ok, err := repo.CountdownAck(ctx, first)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.CountdownAck(ctx, next)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestCountdownRestartBeforePromotionAndCap(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	ctx := context.Background()
	_, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 8, Name: "demo", Verb: "start", Seconds: 1, OperationID: "start"})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE countdowns SET expires_at_ms=?", time.Now().Add(-time.Second).UnixMilli())
	require.NoError(t, err)
	fresh, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 8, Name: "demo", Verb: "start", Seconds: 2592000, OperationID: "restart"})
	require.NoError(t, err)
	_, found, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.False(t, found)
	capped, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 8, Name: "demo", Verb: "add", Seconds: 2592000, OperationID: "add"})
	require.NoError(t, err)
	require.True(t, capped.Found)
	require.True(t, capped.Capped)
	require.GreaterOrEqual(t, capped.ExpiresAtMS, fresh.ExpiresAtMS)
}

func TestCountdownOperationRetention(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	ctx := context.Background()
	_, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "demo", Verb: "start", Seconds: 1, OperationID: "old"})
	require.NoError(t, err)
	_, err = repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "demo", Verb: "start", Seconds: 1, OperationID: "recent"})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE countdown_operations SET created_at_ms=? WHERE operation_id=?", time.Now().Add(-32*24*time.Hour).UnixMilli(), "old")
	require.NoError(t, err)
	_, _, err = repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"old": 1, "recent": 1}, countdownOperationCounts(t, raw))
	require.NoError(t, repo.PruneCountdownOperations(ctx))
	require.Equal(t, map[string]int{"old": 0, "recent": 1}, countdownOperationCounts(t, raw))
}

func countdownOperationCounts(t *testing.T, raw *sql.DB) map[string]int {
	counts := map[string]int{}
	for _, id := range []string{"old", "recent"} {
		var n int
		require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM countdown_operations WHERE operation_id=?", id).Scan(&n))
		counts[id] = n
	}
	return counts
}

func TestCountdownNameLimitAllowsRestartAndFreesCompletedNames(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	ctx := context.Background()
	for i := 0; i < 128; i++ {
		_, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: fmt.Sprintf("timer%d", i), Verb: "start", Seconds: 10, OperationID: fmt.Sprintf("op%d", i)})
		require.NoError(t, err)
	}
	_, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "overflow", Verb: "start", Seconds: 10, OperationID: "overflow"})
	require.ErrorIs(t, err, repository.ErrVariableLimit)
	_, err = repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "timer0", Verb: "start", Seconds: 10, OperationID: "restart"})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE countdowns SET expires_at_ms=? WHERE name=?", time.Now().Add(-time.Second).UnixMilli(), "timer0")
	require.NoError(t, err)
	_, found, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.True(t, found)
	_, err = repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 5, Name: "overflow", Verb: "start", Seconds: 10, OperationID: "overflow"})
	require.NoError(t, err)
}

func TestCountdownProgressSurvivesLeaseAndCannotRegress(t *testing.T) {
	repo, raw := sqliteStateRepo(t)
	ctx := context.Background()
	_, err := repo.CountdownWrite(ctx, repository.CountdownWriteRequest{UserID: 9, Name: "demo", Verb: "start", Seconds: 1, OperationID: "start"})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE countdowns SET expires_at_ms=?", time.Now().Add(-time.Second).UnixMilli())
	require.NoError(t, err)
	first, ok, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.CountdownProgress(ctx, repository.CountdownProgressRequest{Delivery: first, Key: "recipe1", Status: "conditions_passed"})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.CountdownProgress(ctx, repository.CountdownProgressRequest{Delivery: first, Key: "recipe1", Status: "completed"})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.CountdownProgress(ctx, repository.CountdownProgressRequest{Delivery: first, Key: "recipe1", Status: "conditions_passed"})
	require.NoError(t, err)
	require.True(t, ok)
	_, err = raw.Exec("UPDATE countdown_deliveries SET lease_until_ms=0")
	require.NoError(t, err)
	next, ok, err := repo.CountdownClaim(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "completed", next.Progress["recipe1"])
	ok, err = repo.CountdownProgress(ctx, repository.CountdownProgressRequest{Delivery: first, Key: "recipe2", Status: "completed"})
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.CountdownProgress(ctx, repository.CountdownProgressRequest{Delivery: next, Key: "recipe2", Status: "conditions_passed"})
	require.NoError(t, err)
	require.True(t, ok)
}
