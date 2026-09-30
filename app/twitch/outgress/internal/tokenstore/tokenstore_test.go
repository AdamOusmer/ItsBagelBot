// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tokenstore

import (
	"context"
	"testing"
	"time"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func connectBroker(t *testing.T) *nats.Conn {
	t.Helper()
	broker, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	broker.Start()
	t.Cleanup(broker.Shutdown)
	require.True(t, broker.ReadyForConnections(2*time.Second))
	nc, err := nats.Connect(broker.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

func serveTokensGet(t *testing.T, nc *nats.Conn, prefix string, get func(context.Context, usersrpc.TokensRequest, uint64) (usersrpc.TokensReply, error)) {
	t.Helper()
	wiring := bus.RPCWiring{NC: nc, Queue: "tokens-test", Log: zap.NewNop()}
	require.NoError(t, bus.ServeVerbs(wiring, prefix, bus.VerbForUser[usersrpc.TokensRequest, usersrpc.TokensReply]("get", get)))
}

func replyingWith(reply usersrpc.TokensReply) func(t *testing.T, nc *nats.Conn, prefix string) {
	return func(t *testing.T, nc *nats.Conn, prefix string) {
		serveTokensGet(t, nc, prefix, func(context.Context, usersrpc.TokensRequest, uint64) (usersrpc.TokensReply, error) {
			return reply, nil
		})
	}
}

type loadRefusalCase struct {
	name            string
	serve           func(t *testing.T, nc *nats.Conn, prefix string)
	wantErr         bool
	wantUnavailable bool
}

var loadRefusalCases = []loadRefusalCase{
	{
		name:  "stored token loads",
		serve: replyingWith(usersrpc.TokensReply{RefreshToken: "refresh"}),
	},
	{
		name:    "not_found refusal keeps the no-token path",
		serve:   replyingWith(usersrpc.TokensReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "ent: tokens not found")}),
		wantErr: true,
	},
	{
		name:            "internal refusal is an outage",
		serve:           replyingWith(usersrpc.TokensReply{Refusal: domainrpc.Refused(domainrpc.CodeInternal, "db exploded")}),
		wantErr:         true,
		wantUnavailable: true,
	},
	{
		name:    "uncoded not found refusal from an old users service keeps the no-token path",
		serve:   replyingWith(usersrpc.TokensReply{Refusal: domainrpc.Refusal{Error: "tokens not found for user"}}),
		wantErr: true,
	},
	{
		name:            "uncoded internal refusal from an old users service is an outage",
		serve:           replyingWith(usersrpc.TokensReply{Refusal: domainrpc.Refusal{Error: "db exploded"}}),
		wantErr:         true,
		wantUnavailable: true,
	},
	{
		name:            "no responders is an outage",
		serve:           func(*testing.T, *nats.Conn, string) {},
		wantErr:         true,
		wantUnavailable: true,
	},
	{
		name: "silent users service is an outage",
		serve: func(t *testing.T, nc *nats.Conn, prefix string) {
			require.NoError(t, bus.QueueSubscribeRPC(nc, prefix+".get", "tokens-test", func(*nats.Msg) {}))
		},
		wantErr:         true,
		wantUnavailable: true,
	},
}

func TestLoadSeparatesRefusalFromOutage(t *testing.T) {
	for _, tc := range loadRefusalCases {
		t.Run(tc.name, func(t *testing.T) {
			nc := connectBroker(t)
			tc.serve(t, nc, "tokens")
			require.NoError(t, nc.Flush())

			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			_, err := New(nc, "tokens", "42").Load(ctx)

			require.Equal(t, tc.wantErr, err != nil, "Load error: %v", err)
			require.Equal(t, tc.wantUnavailable, Unavailable(err), "Unavailable(%v)", err)
		})
	}
}
