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
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAcceptanceIsReadBackFromEitherReplica(t *testing.T) {
	broker, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	broker.Start()
	t.Cleanup(broker.Shutdown)
	require.True(t, broker.ReadyForConnections(2*time.Second))
	nc, err := nats.Connect(broker.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	client := testdb.Open(t, "dashboard-onboarded", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	writer := repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	reader := repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { writer.Close(context.Background()); reader.Close(context.Background()) })
	require.NoError(t, writer.Register(t.Context(), 1001, "mavey", "Mavey", "mavey@example.com"))
	subscribe := func(repo *repository.Users, prefix string) {
		require.NoError(t, SubscribeDashboard(Wiring{RPCWiring: bus.RPCWiring{NC: nc, Queue: prefix, Log: zap.NewNop()}, Repo: repo}, prefix, "invalidate."+prefix))
	}
	subscribe(writer, "dashboard.writer")
	subscribe(reader, "dashboard.reader")
	require.NoError(t, nc.Flush())

	request := func(subject string, req any) map[string]any {
		body, err := codec.Marshal(req)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		msg, err := bus.RequestWithContext(ctx, nc, subject, body)
		require.NoError(t, err)
		var reply map[string]any
		require.NoError(t, codec.Unmarshal(msg.Data, &reply))
		return reply
	}
	state := usersrpc.StateGetRequest{BroadcasterUserID: "1001"}

	require.Equal(t, false, request("dashboard.reader.state_get", state)["onboarded"])
	request("dashboard.writer.onboarded_set", usersrpc.OnboardedSetRequest{BroadcasterUserID: "1001", Onboarded: true})
	require.Equal(t, []any{true, true}, []any{
		request("dashboard.writer.state_get", state)["onboarded"],
		request("dashboard.reader.state_get", state)["onboarded"],
	})
}
