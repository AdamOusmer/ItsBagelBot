// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/notifications/ent/enttest"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/internal/domain/invalidate"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMarkReadPublishesNotificationsInvalidation(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { client.Close() })
	broadcast := client.Notification.Create().SetScope(notification.ScopeBroadcast).SetTitle("broadcast").SetBody("body").SetCreatedBy(1).SetCreatedByLogin("admin").SaveX(t.Context())
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	_, err = nc.Subscribe("test.users.get", func(m *nats.Msg) {
		body, _ := codec.Marshal(usersrpc.AdminReply{User: &usersrpc.AdminUserView{ID: 42, CreatedAt: time.Now().Add(-time.Hour)}})
		_ = m.Respond(body)
	})
	require.NoError(t, err)
	invalidations, err := nc.SubscribeSync("test.invalidate.notifications")
	require.NoError(t, err)

	wiring := Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "test"}, Repo: repository.New(client)}
	require.NoError(t, SubscribeUser(wiring, UserConfig{UserGetSubject: "test.users.get", Prefix: "test.notifications", InvalidationPrefix: "test.invalidate", FullReadTTL: time.Hour, PeekTTL: time.Hour}))
	require.NoError(t, nc.Flush())

	_, err = bus.RequestJSONTimeout[notificationsrpc.MarkReadReply](t.Context(), nc, "test.notifications.mark_read", notificationsrpc.MarkReadRequest{UserID: "42", NotificationID: strconv.Itoa(broadcast.ID)}, time.Second)
	require.NoError(t, err)

	msg, err := invalidations.NextMsg(time.Second)
	require.NoError(t, err)
	var dto invalidate.DTO
	require.NoError(t, codec.Unmarshal(msg.Data, &dto))
	require.Equal(t, "42", dto.BroadcasterID)
}
