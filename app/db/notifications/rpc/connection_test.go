// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/notifications/ent/enttest"
	"ItsBagelBot/app/db/notifications/repository"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"context"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestEraseRPCReturnsErasedAcknowledgment(t *testing.T) {
	client := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { client.Close() })
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	require.NoError(t, SubscribeConnection(Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "test"}, Repo: repository.New(client)}, "test.invalidate"))
	require.NoError(t, nc.Flush())
	reply, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](context.Background(), nc, notificationsrpc.ConnectionSubject+".erase", notificationsrpc.ConnectionRequest{AccountID: 123, AccountCreatedAt: time.Now().Add(-time.Hour).UnixMicro(), ErasedAt: time.Now()}, time.Second)
	require.NoError(t, err)
	require.Equal(t, notificationsrpc.OutcomeErased, reply.Outcome)
}
