// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	entcore "entgo.io/ent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestAccountAuditSurvivesAdminOutageAndLostAcknowledgment(t *testing.T) {
	client, _, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 10, "target", "Target", "target@example.com"))
	service := staffService(t, repo, []usersrpc.AdminAcctView{{ID: 12, Active: true, Role: "admin", Login: "admin"}})
	service.unavailable.Store(true)
	require.NoError(t, repo.SetTestAccount(ctx, 10, true, 12))
	assert.True(t, client.User.GetX(ctx, 10).TestAccount)
	assert.Equal(t, 1, client.AdminAuditDelivery.Query().CountX(ctx))
	service.unavailable.Store(false)
	service.revoked.Store(true)
	var failDelete atomic.Bool
	failDelete.Store(true)
	client.AdminAuditDelivery.Use(func(next entcore.Mutator) entcore.Mutator {
		return entcore.MutateFunc(func(ctx context.Context, mutation entcore.Mutation) (entcore.Value, error) {
			if mutation.Op().Is(entcore.OpDelete) && failDelete.Swap(false) {
				return nil, errors.New("delete acknowledgment lost")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	_, err := repo.DeliverAdminAudits(ctx, time.Now())
	require.Error(t, err)
	assert.Equal(t, 1, client.AdminAuditDelivery.Query().CountX(ctx))
	got := <-service.audit
	require.NotEmpty(t, got.AuditID)
	assert.Equal(t, usersrpc.AuthRequest{AuditID: got.AuditID, ActorID: "12", ActorLogin: "admin", Action: "set_test_account", Target: "10", Detail: "enabled=true", OK: true}, got)
	count, err := repo.DeliverAdminAudits(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, [3]int{1, 0, 0}, [3]int{count, client.AdminAuditDelivery.Query().CountX(ctx), len(service.audit)})
}

func TestTestAccountAndAuditIntentCommitTogether(t *testing.T) {
	client, _, repo := setup(t)
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 10, "target", "Target", "target@example.com"))
	service := staffService(t, repo, []usersrpc.AdminAcctView{{ID: 12, Active: true, Role: "admin", Login: "admin"}})
	client.AdminAuditDelivery.Use(func(entcore.Mutator) entcore.Mutator {
		return entcore.MutateFunc(func(context.Context, entcore.Mutation) (entcore.Value, error) {
			return nil, errors.New("audit intent unavailable")
		})
	})
	require.Error(t, repo.SetTestAccount(ctx, 10, true, 12))
	assert.False(t, client.User.GetX(ctx, 10).TestAccount)
	assert.Equal(t, [2]int{0, 0}, [2]int{client.AdminAuditDelivery.Query().CountX(ctx), len(service.audit)})
}
