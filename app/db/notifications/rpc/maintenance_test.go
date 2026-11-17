// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/enttest"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/repository"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCleanupRPCReportsSelectedBatchDespiteConcurrentRead(t *testing.T) {
	for _, count := range []int{repository.CleanupBatchSize - 1, repository.CleanupBatchSize, repository.CleanupBatchSize + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			client := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
			t.Cleanup(func() { _ = client.Close() })
			repo := repository.New(client)
			id := seedExpiredNotifications(t, client, count)
			inserted := false
			client.Notification.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if !inserted {
						inserted = true
						require.NoError(t, repo.MarkRead(ctx, id, 42, time.Now().Add(time.Hour)))
					}
					return next.Mutate(ctx, m)
				})
			})
			nc := testnats.Connect(t)
			wiring := Wiring{RPCWiring: bus.RPCWiring{NC: nc, Queue: "test", Log: zap.NewNop()}, Repo: repo}
			require.NoError(t, SubscribeMaintenance(wiring, "test.cleanup"))
			reply, err := bus.RequestJSON[notificationsrpc.CleanupReply](t.Context(), nc, "test.cleanup", notificationsrpc.CleanupRequest{})
			require.NoError(t, err)
			assert.Equal(t, notificationsrpc.CleanupReply{
				Deleted: min(count, repository.CleanupBatchSize) - 1,
				HasMore: count >= repository.CleanupBatchSize,
			}, reply)
			if !reply.HasMore {
				return
			}
			reply, err = bus.RequestJSON[notificationsrpc.CleanupReply](t.Context(), nc, "test.cleanup", notificationsrpc.CleanupRequest{})
			require.NoError(t, err)
			assert.Equal(t, notificationsrpc.CleanupReply{Deleted: count - repository.CleanupBatchSize + 1}, reply)
		})
	}
}

func seedExpiredNotifications(t *testing.T, client *ent.Client, count int) int {
	t.Helper()
	builders := make([]*ent.NotificationCreate, count)
	for i := range builders {
		builders[i] = client.Notification.Create().SetScope(notification.ScopeBroadcast).
			SetTitle("Expired").SetBody("Body").SetCreatedBy(1).SetCreatedByLogin("admin").SetExpiresAt(time.Now().Add(-time.Hour))
	}
	var first int
	for chunk := range slices.Chunk(builders, 100) {
		rows, err := client.Notification.CreateBulk(chunk...).Save(t.Context())
		require.NoError(t, err)
		if first == 0 {
			first = rows[0].ID
		}
	}
	return first
}
