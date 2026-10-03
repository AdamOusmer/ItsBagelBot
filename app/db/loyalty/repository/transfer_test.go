// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"math"

	"testing"

	baseent "entgo.io/ent"

	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalanceTransferMovesPoints(t *testing.T) {
	repo, client, ctx := fundedLoyalty(t, 1000)
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

func TestBalanceTransferRefusals(t *testing.T) {
	for _, tc := range []struct {
		name         string
		seedReceiver bool
		transfer     loyaltyrepo.Transfer
		wantFound    bool
	}{
		{
			name:         "TestBalanceTransferRefusesShortfall",
			seedReceiver: true,
			transfer:     loyaltyrepo.Transfer{TargetLogin: "receiver", Amount: 400},
			wantFound:    true,
		},
		{
			name:      "TestBalanceTransferShortfallDoesNotCreateRecipient",
			transfer:  loyaltyrepo.Transfer{TargetViewerID: 8, TargetLogin: "newviewer", Amount: 200},
			wantFound: true,
		},
		{
			name:     "TestBalanceTransferUnknownTarget",
			transfer: loyaltyrepo.Transfer{TargetLogin: "ghost", Amount: 10},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, client, ctx := fundedLoyalty(t, 100)
			if tc.seedReceiver {
				seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "receiver"})
			}
			tc.transfer.UserID, tc.transfer.FromViewerID = 2, 7

			out, found, err := repo.BalanceTransfer(ctx, tc.transfer)

			require.NoError(t, err)
			assert.Equal(t, tc.wantFound, found)
			if tc.wantFound {
				require.NotNil(t, out)
				assert.Nil(t, out.To, "a refused move credits nobody")
				assert.Equal(t, int64(100), out.From.Points)
			} else {
				assert.Nil(t, out)
			}
			sender, _, err := repo.BalanceGet(ctx, 2, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(100), sender.Points, "the debit must not land")
			receiver, exists, err := repo.BalanceGet(ctx, 2, 8)
			require.NoError(t, err)
			assert.Equal(t, tc.seedReceiver, exists, "a refused move creates no recipient")
			if exists {
				assert.Zero(t, receiver.Points)
			}
		})
	}
}

func TestBalanceTransferRefusesSelfAndBadInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		transfer loyaltyrepo.Transfer
	}{
		{"refuses a transfer to oneself by login", loyaltyrepo.Transfer{TargetLogin: "sender", Amount: 10}},
		{"refuses a blank target login", loyaltyrepo.Transfer{Amount: 10}},
		{"refuses a zero amount", loyaltyrepo.Transfer{TargetLogin: "receiver"}},
		{"refuses a negative amount", loyaltyrepo.Transfer{TargetLogin: "receiver", Amount: -5}},
		{"TestBalanceTransferRejectsResolvedSelf", loyaltyrepo.Transfer{TargetViewerID: 7, TargetLogin: "renamed", Amount: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, ctx := fundedLoyalty(t, 100)
			tc.transfer.UserID, tc.transfer.FromViewerID = 2, 7
			_, _, err := repo.BalanceTransfer(ctx, tc.transfer)
			assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
		})
	}
}

func TestBalanceTransferFromUnknownSenderIsInsufficient(t *testing.T) {
	repo, _, ctx := fundedLoyalty(t, 100)
	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 99, TargetLogin: "sender", Amount: 10})
	require.NoError(t, err)
	assert.True(t, found)
	require.NotNil(t, out)
	assert.Equal(t, uint64(99), out.From.ViewerID)
	assert.Zero(t, out.From.Points)
	assert.Nil(t, out.To)
}

func TestBalanceTransferCreatesResolvedRecipient(t *testing.T) {
	repo, client, ctx := fundedLoyalty(t, 1000)
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
	repo, client, ctx := fundedLoyalty(t, 1000)
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

func TestBalanceTransferCreditFailureRollsBackDebit(t *testing.T) {
	repo, client, ctx := fundedLoyalty(t, 1000)
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

func TestBalanceTransferRetriesALockConflict(t *testing.T) {
	for _, conflict := range []uint16{1213, 1205} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			repo, client, ctx := fundedLoyalty(t, 1000)
			seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "receiver", Points: 100})
			client.Balance.Use(failFirstUpdate(&mysql.MySQLError{Number: conflict}))
			out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetLogin: "receiver", Amount: 400})
			require.NoError(t, err)
			require.True(t, found)
			assert.EqualValues(t, 600, out.From.Points)
			assert.EqualValues(t, 500, out.To.Points)
		})
	}
}

func TestBalanceTransferBIGINTBoundary(t *testing.T) {
	repo, _, ctx := fundedLoyalty(t, math.MaxInt64)
	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "newviewer", Amount: math.MaxInt64})
	require.NoError(t, err)
	require.True(t, found)
	assert.Zero(t, out.From.Points)
	assert.Equal(t, int64(math.MaxInt64), out.To.Points)
}

func TestBalanceTransferOverflowRollsBack(t *testing.T) {
	repo, client, ctx := fundedLoyalty(t, 1000)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "receiver", Points: math.MaxInt64})
	_, _, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "receiver", Amount: 1})
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
	from, _, err := repo.BalanceGet(ctx, 2, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), from.Points)
	to, _, err := repo.BalanceGet(ctx, 2, 8)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), to.Points)
}
