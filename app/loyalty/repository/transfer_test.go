// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"ItsBagelBot/app/loyalty/ent"
	// The generated defaults (created_at etc.) are wired by this package's
	// init; without it every create panics on a nil default func.
	_ "ItsBagelBot/app/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/loyalty/repository"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	entsql "entgo.io/ent/dialect/sql"
)

// newLoyaltyRepo opens an in-memory sqlite-backed repository and hands back
// the client too, so tests can seed and assert through it directly. The DB is
// opened by hand (not enttest) because NewLoyalty needs the *entsql.Driver
// its bulk flush statements run through.
func newLoyaltyRepo(t *testing.T) (*loyaltyrepo.Loyalty, *ent.Client) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:loyaltytransfer?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	repo := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	return repo, client
}

func seedBalance(t *testing.T, client *ent.Client, userID, viewerID uint64, login string, points int64) {
	t.Helper()
	err := client.Balance.Create().
		SetUserID(userID).
		SetViewerID(viewerID).
		SetViewerLogin(login).
		SetPoints(points).
		Exec(context.Background())
	require.NoError(t, err)
}

func TestBalanceTransferMovesPoints(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()

	seedBalance(t, client, 2, 7, "sender", 1000)
	seedBalance(t, client, 2, 8, "receiver", 100)

	out, found, err := repo.BalanceTransfer(ctx, 2, 7, "@Receiver", 400)
	require.NoError(t, err)
	assert.True(t, found)
	require.NotNil(t, out)
	require.NotNil(t, out.From)
	require.NotNil(t, out.To)
	assert.Equal(t, int64(600), out.From.Points)
	assert.Equal(t, int64(500), out.To.Points)
	assert.Equal(t, uint64(7), out.From.ViewerID)
	assert.Equal(t, uint64(8), out.To.ViewerID)
}

func TestBalanceTransferRefusesShortfall(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()

	seedBalance(t, client, 2, 7, "sender", 100)
	seedBalance(t, client, 2, 8, "receiver", 0)

	out, found, err := repo.BalanceTransfer(ctx, 2, 7, "receiver", 400)
	require.NoError(t, err)
	assert.True(t, found)
	require.NotNil(t, out)
	assert.Nil(t, out.To, "a refused move credits nobody")
	require.NotNil(t, out.From)
	assert.Equal(t, int64(100), out.From.Points, "the debit must not land")
}

func TestBalanceTransferUnknownTarget(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()

	seedBalance(t, client, 2, 7, "sender", 100)

	out, found, err := repo.BalanceTransfer(ctx, 2, 7, "ghost", 10)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, out)

	// The sender's balance is untouched by a failed lookup.
	bal, err := client.Balance.Query().Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(100), bal.Points)
}

func TestBalanceTransferRefusesSelfAndBadInput(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()

	seedBalance(t, client, 2, 7, "sender", 100)

	_, _, err := repo.BalanceTransfer(ctx, 2, 7, "sender", 10)
	require.Error(t, err)
	assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)

	for _, tc := range []struct {
		login  string
		amount int64
	}{
		{"", 10},
		{"receiver", 0},
		{"receiver", -5},
	} {
		_, _, err := repo.BalanceTransfer(ctx, 2, 7, tc.login, tc.amount)
		assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput, "login=%q amount=%d", tc.login, tc.amount)
	}

	// A sender with no row is "never seen here".
	out, found, err := repo.BalanceTransfer(ctx, 2, 99, "sender", 10)
	assert.False(t, found)
	assert.Nil(t, out)
	assert.NoError(t, err)
}
