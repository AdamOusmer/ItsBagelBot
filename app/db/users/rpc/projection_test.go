// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWatchProjectionDistinguishesDeletedUserFromUnavailableSource(t *testing.T) {
	broker, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	broker.Start()
	t.Cleanup(broker.Shutdown)
	require.True(t, broker.ReadyForConnections(2*time.Second))
	nc, err := nats.Connect(broker.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	client := testdb.Open(t, "watch-projection-rpc", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	require.NoError(t, SubscribeProjection(Wiring{RPCWiring: bus.RPCWiring{NC: nc, Queue: "projection-test", Log: zap.NewNop()}, Repo: repo}, "projection.users.get"))
	require.NoError(t, nc.Flush())
	request := func(id string) projection.UserReply {
		body, err := codec.Marshal(projection.Request{UserID: id})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		msg, err := bus.RequestWithContext(ctx, nc, "projection.users.get", body)
		require.NoError(t, err)
		var reply projection.UserReply
		require.NoError(t, codec.Unmarshal(msg.Data, &reply))
		return reply
	}
	missing := request("1001")
	require.Equal(t, domainrpc.CodeNotFound, missing.Code)
	require.Equal(t, "1001", missing.UserID)
	require.Zero(t, missing.AccountCreatedAt)
	require.NoError(t, client.Close())
	unavailable := request("1002")
	require.NotEmpty(t, unavailable.Error)
	require.NotEqual(t, domainrpc.CodeNotFound, unavailable.Code, "a failed authority must never authorize destructive cleanup")
}
