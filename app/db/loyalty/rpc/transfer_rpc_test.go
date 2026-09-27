// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"database/sql"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	entsql "entgo.io/ent/dialect/sql"
)

func newTransferHarness(t *testing.T) *loyaltyRPC {
	t.Helper()
	db, err := sql.Open(testdb.Driver, testdb.MemDSN("loyaltytransferrpc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("sqlite3", db)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(context.Background()))
	ctx := context.Background()
	for _, seed := range []struct {
		viewerID uint64
		login    string
		points   int64
	}{
		{7, "sender", 1000},
		{8, "receiver", 100},
	} {
		require.NoError(t, client.Balance.Create().
			SetUserID(2).
			SetViewerID(seed.viewerID).
			SetViewerLogin(seed.login).
			SetPoints(seed.points).
			Exec(ctx))
	}
	repo := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	return &loyaltyRPC{repo: repo, log: zap.NewNop()}
}

func TestHandleBalanceTransferMovesPoints(t *testing.T) {
	l := newTransferHarness(t)

	reply := l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "@Receiver", Value: 400,
	})
	assert.Empty(t, reply.Error)
	assert.True(t, reply.Found)
	assert.True(t, reply.Spent)
	require.NotNil(t, reply.Balance)
	assert.Equal(t, int64(600), reply.Balance.Points, "sender after")
	require.NotNil(t, reply.TargetBalance)
	assert.Equal(t, int64(500), reply.TargetBalance.Points, "receiver after")
}

func TestHandleBalanceTransferRefusals(t *testing.T) {
	l := newTransferHarness(t)

	reply := l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "receiver", Value: 5000,
	})
	assert.Empty(t, reply.Error)
	assert.True(t, reply.Found)
	assert.False(t, reply.Spent)
	assert.Nil(t, reply.TargetBalance)
	assert.Equal(t, int64(1000), reply.Balance.Points)

	reply = l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "ghost", Value: 10,
	})
	assert.Empty(t, reply.Error)
	assert.False(t, reply.Found)

	reply = l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "sender", Value: 10,
	})
	assert.NotEmpty(t, reply.Error)

	reply = l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerLogin: "receiver", Value: 10,
	})
	assert.NotEmpty(t, reply.Error)
}

func TestHandleBalanceTransferResolvedRecipient(t *testing.T) {
	l := newTransferHarness(t)
	reply := l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "9", ViewerLogin: "blemmyz", Value: 400})
	require.Empty(t, reply.Error)
	require.True(t, reply.Spent)
	assert.Equal(t, int64(600), reply.Balance.Points)
	assert.Equal(t, "9", reply.TargetBalance.ViewerID)
	assert.Equal(t, int64(400), reply.TargetBalance.Points)
}

func TestHandleBalanceTransferMissingSenderIsInsufficient(t *testing.T) {
	l := newTransferHarness(t)
	reply := l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{UserID: "2", ViewerID: "99", TargetViewerID: "9", ViewerLogin: "blemmyz", Value: 400})
	require.Empty(t, reply.Error)
	assert.True(t, reply.Found)
	assert.False(t, reply.Spent)
	require.NotNil(t, reply.Balance)
	assert.Equal(t, "99", reply.Balance.ViewerID)
	assert.Zero(t, reply.Balance.Points)
	assert.Nil(t, reply.TargetBalance)
}

func TestHandleBalanceTransferRejectsInvalidTargetID(t *testing.T) {
	l := newTransferHarness(t)
	for _, id := range []string{"0", "-1", "nope", "18446744073709551616", "7"} {
		reply := l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: id, ViewerLogin: "renamed", Value: 400})
		assert.NotEmpty(t, reply.Error, id)
		assert.False(t, reply.Spent, id)
	}
}
