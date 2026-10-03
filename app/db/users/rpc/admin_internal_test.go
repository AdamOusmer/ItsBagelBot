// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/premiumgrant"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupAdminRPCTest(t *testing.T) (*adminRPC, *ent.Client) {
	t.Helper()

	client := testdb.Open(t, "adminrpc", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })

	repo := repository.NewUsers(client, nil, nil, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	return &adminRPC{repo: repo, log: zap.NewNop()}, client
}

func createAdminUser(t *testing.T, client *ent.Client, i int, updatedAt time.Time) {
	t.Helper()

	client.User.Create().
		SetID(uint64(9000 + i)).
		SetUsername(fmt.Sprintf("user-%02d", i)).
		SetEmail(fmt.Sprintf("user-%02d@example.invalid", i)).
		SetStatus(user.StatusFree).
		SetUpdatedAt(updatedAt).
		ExecX(context.Background())
}

func handlerFor(t *testing.T, a *adminRPC, verb string) func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply {
	t.Helper()
	for _, v := range a.verbs() {
		if v.name == verb {
			return a.guarded(v)
		}
	}
	t.Fatalf("no admin verb named %q", verb)
	return nil
}

func TestAdminListAndStatsReadStoredStatus(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
	now := time.Now().UTC()

	client.User.Create().
		SetID(1514230534).
		SetUsername("winksmc").
		SetEmail("winksmc@example.invalid").
		SetStatus(user.StatusFree).
		SetUpdatedAt(now).
		ExecX(ctx)
	client.PremiumGrant.Create().
		SetUserID(1514230534).
		SetGiveawayID("g-admin").
		SetAwardID("a-admin").
		SetState(premiumgrant.StateCommitted).
		SetStartAt(now.Add(-time.Hour)).
		SetEndAt(now.AddDate(0, 1, 0)).
		SetIntervalRuleVersion("promotional-calendar-month-v1").
		ExecX(ctx)

	got := a.get(ctx, usersrpc.AdminRequest{UserID: "1514230534"})
	require.Empty(t, got.Error)
	require.NotNil(t, got.User)
	assert.Equal(t, "free", got.User.Status, "admin reads users.status; a grant does not overlay")

	paid := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize, State: "paid"})
	require.Empty(t, paid.Error)
	assert.Empty(t, replyIDs(paid))

	client.User.UpdateOneID(1514230534).SetStatus(user.StatusPaid).ExecX(ctx)

	got = a.get(ctx, usersrpc.AdminRequest{UserID: "1514230534"})
	require.Empty(t, got.Error)
	assert.Equal(t, "paid", got.User.Status)

	paid = a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize, State: "paid"})
	require.Empty(t, paid.Error)
	assert.Equal(t, []uint64{1514230534}, replyIDs(paid))

	stats := a.stats(ctx, usersrpc.AdminRequest{})
	require.Empty(t, stats.Error)
	require.NotNil(t, stats.Stats)
	assert.Equal(t, 1, stats.Stats.PaidUsers)
	assert.Equal(t, 1, stats.Stats.PremiumUsers)
}

func replyIDs(reply usersrpc.AdminReply) []uint64 {
	ids := make([]uint64, 0, len(reply.Users))
	for _, u := range reply.Users {
		ids = append(ids, u.ID)
	}
	return ids
}

func TestAdminActiveToggleEchoesCommittedState(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	createAdminUser(t, client, 0, time.Now())
	a.repo = repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { a.repo.Close(context.Background()) })
	for _, active := range []bool{false, true} {
		reply := a.setActive(t.Context(), usersrpc.AdminRequest{UserID: "9000", Active: active})
		require.Empty(t, reply.Error)
		require.NotNil(t, reply.User)
		require.Equal(t, active, reply.User.IsActive, "action reply must not restore the old optimistic toggle value")
		require.Equal(t, active, client.User.GetX(t.Context(), 9000).IsActive)
	}
}
