// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"errors"

	baseent "entgo.io/ent"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	entsql "entgo.io/ent/dialect/sql"
)

func newLoyaltyRepo(t *testing.T) (*loyaltyrepo.Loyalty, *ent.Client) {
	t.Helper()
	db, err := sql.Open(testdb.Driver, testdb.MemDSN("loyaltytransfer"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	repo := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	return repo, client
}

type seedRow struct {
	UserID, ViewerID uint64
	Login            string
	Points           int64
}

func seedBalance(t *testing.T, client *ent.Client, row seedRow) {
	t.Helper()
	err := client.Balance.Create().
		SetUserID(row.UserID).
		SetViewerID(row.ViewerID).
		SetViewerLogin(row.Login).
		SetPoints(row.Points).
		Exec(context.Background())
	require.NoError(t, err)
}

func TestBalanceTransferMovesPoints(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()

	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 1000})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "receiver", Points: 100})

	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetLogin: "@Receiver", Amount: 400})
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

	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 100})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "receiver", Points: 0})

	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetLogin: "receiver", Amount: 400})
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

	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 100})

	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetLogin: "ghost", Amount: 10})
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, out)

	bal, err := client.Balance.Query().Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(100), bal.Points)
}

func TestBalanceTransferRefusesSelfAndBadInput(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()

	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 100})

	_, _, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetLogin: "sender", Amount: 10})
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
		_, _, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetLogin: tc.login, Amount: tc.amount})
		assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput, "login=%q amount=%d", tc.login, tc.amount)
	}

	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 99, TargetLogin: "sender", Amount: 10})
	require.NoError(t, err)
	assert.True(t, found)
	require.NotNil(t, out)
	assert.Equal(t, int64(0), out.From.Points)
	assert.Equal(t, uint64(99), out.From.ViewerID)
	assert.Nil(t, out.To)
}

func TestBalanceTransferCreatesResolvedRecipient(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 1000})
	// The same viewer's balance in another channel must stay independent.
	seedBalance(t, client, seedRow{UserID: 3, ViewerID: 8, Login: "blemmyz", Points: 20})
	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "@Blemmyz", Amount: 400})
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, out.To)
	assert.Equal(t, int64(600), out.From.Points)
	assert.Equal(t, int64(400), out.To.Points)
	assert.Equal(t, uint64(8), out.To.ViewerID)
	assert.Equal(t, "blemmyz", out.To.ViewerLogin)
	other, _, err := repo.BalanceGet(ctx, 3, 8)
	require.NoError(t, err)
	assert.Equal(t, int64(20), other.Points)
}

func TestBalanceTransferResolvedIdentityPreservesExistingBalance(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 1000})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "oldlogin", Points: 100})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 9, Login: "receiver", Points: 70})
	require.NoError(t, client.Balance.Update().SetViewerName("Receiver").SetWatchSeconds(123).Exec(ctx))
	for range 2 {
		out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "receiver", Amount: 200})
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, out.To)
	}
	to, _, err := repo.BalanceGet(ctx, 2, 8)
	require.NoError(t, err)
	assert.Equal(t, int64(500), to.Points)
	assert.Equal(t, "receiver", to.ViewerLogin)
	assert.Equal(t, "Receiver", to.ViewerName)
	assert.Equal(t, uint64(123), to.WatchSeconds)
	stale, _, err := repo.BalanceGet(ctx, 2, 9)
	require.NoError(t, err)
	assert.Equal(t, int64(70), stale.Points, "stored login must not override resolved Twitch ID")
}

func TestBalanceTransferShortfallDoesNotCreateRecipient(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 100})
	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "newviewer", Amount: 200})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, int64(100), out.From.Points)
	assert.Nil(t, out.To)
	_, exists, err := repo.BalanceGet(ctx, 2, 8)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestBalanceTransferCreditFailureRollsBackDebit(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := context.Background()
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 1000})
	creditErr := errors.New("recipient creation failed")
	client.Balance.Use(func(next baseent.Mutator) baseent.Mutator {
		return baseent.MutateFunc(func(ctx context.Context, m baseent.Mutation) (baseent.Value, error) {
			if m.Op().Is(baseent.OpCreate) {
				return nil, creditErr
			}
			return next.Mutate(ctx, m)
		})
	})
	_, _, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "newviewer", Amount: 400})
	require.ErrorIs(t, err, creditErr)
	from, _, err := repo.BalanceGet(ctx, 2, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), from.Points)
	_, exists, err := repo.BalanceGet(ctx, 2, 8)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestBalanceTransferRejectsResolvedSelf(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	_, _, err := repo.BalanceTransfer(context.Background(), loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 7, TargetLogin: "renamed", Amount: 10})
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
}
