package rpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/enttest"
	"ItsBagelBot/app/users/ent/user"
	"ItsBagelBot/app/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupAdminRPCTest(t *testing.T) (*adminRPC, *ent.Client) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:adminrpc?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	// packer and pub are nil because the list/search tests do not exercise
	// write or token paths that would call them.
	repo := repository.NewUsers(client, nil, nil)
	t.Cleanup(repo.Close)

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

func TestAdminUserListPagesResults(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		createAdminUser(t, client, i, base.Add(time.Duration(i)*time.Minute))
	}

	first := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize})
	require.Empty(t, first.Error)
	require.Len(t, first.Users, adminUserPageSize)
	assert.Equal(t, 1, first.Page)
	assert.Equal(t, adminUserPageSize, first.PageSize)
	assert.Equal(t, adminUserMaxPages, first.MaxPages)
	assert.True(t, first.HasMore)
	assert.Equal(t, "user-29", first.Users[0].Username)
	assert.Equal(t, fmt.Sprintf("user-%02d", 30-adminUserPageSize), first.Users[adminUserPageSize-1].Username)

	second := a.list(ctx, usersrpc.AdminRequest{Page: 2, Limit: adminUserPageSize})
	require.Empty(t, second.Error)
	require.Len(t, second.Users, adminUserPageSize)
	assert.False(t, second.HasMore)
	assert.Equal(t, fmt.Sprintf("user-%02d", 30-adminUserPageSize-1), second.Users[0].Username)
}

func TestAdminUserListSearchesBeforePaging(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		createAdminUser(t, client, i, base.Add(time.Duration(i)*time.Minute))
	}
	client.User.Create().
		SetID(424242).
		SetUsername("needle-user").
		SetEmail("needle@example.invalid").
		SetStatus(user.StatusVip).
		SetUpdatedAt(base.Add(2 * time.Hour)).
		ExecX(ctx)

	reply := a.list(ctx, usersrpc.AdminRequest{Page: 1, Limit: adminUserPageSize, Search: "NEEDLE"})
	require.Empty(t, reply.Error)
	require.Len(t, reply.Users, 1)
	assert.Equal(t, "needle-user", reply.Users[0].Username)
	assert.Equal(t, uint64(424242), reply.Users[0].ID)
	assert.False(t, reply.HasMore)
}
