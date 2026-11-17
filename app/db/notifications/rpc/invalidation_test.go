// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/enttest"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/app/db/notifications/rpc"
	"ItsBagelBot/internal/domain/invalidate"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type invalidationFixture struct {
	client *ent.Client
	wiring rpc.Wiring
	sub    *nats.Subscription
}

func newInvalidationFixture(t *testing.T) invalidationFixture {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	nc := testnats.Connect(t)
	sub, err := nc.SubscribeSync("test.invalidate.notifications")
	require.NoError(t, err)
	w := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repository.New(client), Invalidations: bus.NewInvalidations(nc, "test.invalidate", nil)}
	return invalidationFixture{client: client, wiring: w, sub: sub}
}

func (f invalidationFixture) nextID(t *testing.T) string {
	t.Helper()
	msg, err := f.sub.NextMsg(time.Second)
	require.NoError(t, err)
	var dto invalidate.DTO
	require.NoError(t, codec.Unmarshal(msg.Data, &dto))
	return dto.BroadcasterID
}

func TestNotificationDeletionInvalidatesAudience(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cleanup bool
		direct  bool
		want    string
	}{
		{"delete direct", false, true, "42"},
		{"delete broadcast", false, false, "*"},
		{"cleanup direct", true, true, "42"},
		{"cleanup broadcast", true, false, "*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInvalidationFixture(t)
			create := f.client.Notification.Create().SetScope(notification.ScopeBroadcast).SetTitle("notice").SetBody("body").SetCreatedBy(1).SetCreatedByLogin("admin").SetExpiresAt(time.Now().Add(-time.Hour))
			if tc.direct {
				create.SetScope(notification.ScopeDirect).SetTargetUserID(42)
			}
			row := create.SaveX(t.Context())
			require.NoError(t, rpc.SubscribeAdmin(f.wiring, rpc.AdminConfig{Prefix: "test.admin"}))
			require.NoError(t, rpc.SubscribeMaintenance(f.wiring, "test.cleanup"))
			subject := "test.admin.delete"
			var request any = notificationsrpc.DeleteRequest{ID: int64(row.ID)}
			if tc.cleanup {
				subject, request = "test.cleanup", notificationsrpc.CleanupRequest{}
			}
			require.NoError(t, f.wiring.NC.Flush())
			reply, err := bus.RequestJSONTimeout[notificationsrpc.DeleteReply](t.Context(), f.wiring.NC, subject, request, time.Second)
			require.NoError(t, err)
			require.Empty(t, reply.Code)
			remaining := f.client.Notification.Query().CountX(t.Context())
			require.Zero(t, remaining)
			require.Equal(t, tc.want, f.nextID(t))
		})
	}
}

func TestPartialPeekFailureInvalidatesCommittedReads(t *testing.T) {
	f := newInvalidationFixture(t)
	for range 2 {
		f.client.Notification.Create().SetScope(notification.ScopeBroadcast).SetTitle("notice").SetBody("body").SetCreatedBy(1).SetCreatedByLogin("admin").ExecX(t.Context())
	}
	var attempts atomic.Int32
	f.client.NotificationRead.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if attempts.Add(1) == 2 {
				return nil, errors.New("second acknowledgement failed")
			}
			return next.Mutate(ctx, m)
		})
	})
	require.NoError(t, bus.Serve(f.wiring.RPCWiring, "test.users", func(context.Context, usersrpc.AdminRequest) usersrpc.AdminReply {
		return usersrpc.AdminReply{User: &usersrpc.AdminUserView{ID: 42, CreatedAt: time.Now().Add(-time.Hour)}}
	}))
	require.NoError(t, rpc.SubscribeUser(f.wiring, rpc.UserConfig{Prefix: "test.notifications", UserGetSubject: "test.users", PeekTTL: time.Hour}))
	require.NoError(t, f.wiring.NC.Flush())
	_, err := bus.RequestJSONTimeout[notificationsrpc.MarkPeekedReply](t.Context(), f.wiring.NC, "test.notifications.mark_peeked", notificationsrpc.MarkPeekedRequest{UserID: "42"}, time.Second)
	require.ErrorContains(t, err, "second acknowledgement failed")
	committed := f.client.NotificationRead.Query().CountX(t.Context())
	require.Equal(t, 1, committed)
	require.Equal(t, "42", f.nextID(t))
}
