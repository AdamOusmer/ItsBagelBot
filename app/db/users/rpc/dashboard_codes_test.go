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

func TestStateGetReplyCodes(t *testing.T) {
	broker, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	broker.Start()
	t.Cleanup(broker.Shutdown)
	require.True(t, broker.ReadyForConnections(2*time.Second))
	nc, err := nats.Connect(broker.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	client := testdb.Open(t, "dashboard-state-codes", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, nil, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	require.NoError(t, repo.Register(t.Context(), 1001, "mavey", "Mavey", "mavey@example.com"))
	require.NoError(t, SubscribeDashboard(Wiring{RPCWiring: bus.RPCWiring{NC: nc, Queue: "dashboard-codes-test", Log: zap.NewNop()}, Repo: repo},
		"dashboard.codes", "invalidate.codes"))
	require.NoError(t, nc.Flush())

	call := func(verb string, req any) domainrpc.Refusal {
		body, err := codec.Marshal(req)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		msg, err := bus.RequestWithContext(ctx, nc, "dashboard.codes."+verb, body)
		require.NoError(t, err)
		var reply domainrpc.Refusal
		require.NoError(t, codec.Unmarshal(msg.Data, &reply))
		return reply
	}
	state := func(id string) any { return usersrpc.StateGetRequest{BroadcasterUserID: id} }
	login := func(name string) any { return map[string]string{"login": name} }

	tests := []struct {
		name string
		verb string
		req  any
		want domainrpc.Code
	}{
		{"existing account has no refusal", "state_get", state("1001"), domainrpc.CodeOK},
		{"malformed id is invalid", "state_get", state("abc"), domainrpc.CodeInvalid},
		{"missing account is not_found", "state_get", state("1002"), domainrpc.CodeNotFound},
		{"unknown login is not_found", "login_resolve", login("nobody"), domainrpc.CodeNotFound},
		{"malformed login is invalid", "login_resolve", login("no spaces"), domainrpc.CodeInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, call(tt.verb, tt.req).Code)
		})
	}

	require.NoError(t, client.Close())
	transient := []domainrpc.Code{domainrpc.CodeInternal, domainrpc.CodeUnavailable}
	t.Run("database failure is a transient code", func(t *testing.T) {
		require.Contains(t, transient, call("state_get", state("1003")).Code)
		require.Contains(t, transient, call("login_resolve", login("mavey")).Code)
	})
}
