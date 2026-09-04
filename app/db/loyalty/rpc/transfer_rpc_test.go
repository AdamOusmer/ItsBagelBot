// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"database/sql"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	// The generated defaults (created_at etc.) are wired by this package's
	// init; without it every create panics on a nil default func.
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	entsql "entgo.io/ent/dialect/sql"
)

// newTransferHarness builds the handler over an in-memory sqlite repository,
// seeded with broadcaster 2's ledger: viewer 7 ("sender") holds 1000 points,
// viewer 8 ("receiver") holds 100.
func newTransferHarness(t *testing.T) *loyaltyRPC {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:loyaltytransferrpc?mode=memory&cache=shared&_fk=1")
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

	// Insufficient: the sender exists and keeps what they had.
	reply := l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "receiver", Value: 5000,
	})
	assert.Empty(t, reply.Error)
	assert.True(t, reply.Found)
	assert.False(t, reply.Spent)
	assert.Nil(t, reply.TargetBalance)
	assert.Equal(t, int64(1000), reply.Balance.Points)

	// Unknown target login.
	reply = l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "ghost", Value: 10,
	})
	assert.Empty(t, reply.Error)
	assert.False(t, reply.Found)

	// Self-transfer is refused as invalid input, not silently no-oped.
	reply = l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerID: "7", ViewerLogin: "sender", Value: 10,
	})
	assert.NotEmpty(t, reply.Error)

	// A missing sender id never reaches the ledger.
	reply = l.handleBalanceTransfer(context.Background(), loyaltyrpc.Request{
		UserID: "2", ViewerLogin: "receiver", Value: 10,
	})
	assert.NotEmpty(t, reply.Error)
}
