// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	domainrpc "ItsBagelBot/internal/domain/rpc"
	commandsrpc "ItsBagelBot/internal/domain/rpc/commands"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commandsResponder(t *testing.T, reply commandsrpc.DashboardReply) (*nats.Conn, <-chan commandsrpc.DashboardRequest) {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	got := make(chan commandsrpc.DashboardRequest, 1)
	_, err = nc.Subscribe("test.commands.upsert", func(msg *nats.Msg) {
		var req commandsrpc.DashboardRequest
		if codec.Unmarshal(msg.Data, &req) == nil {
			got <- req
		}
		body, _ := codec.Marshal(reply)
		_ = msg.Respond(body)
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	return nc, got
}

func TestCommandsRPCUpsertSendsTheWholeRow(t *testing.T) {
	nc, got := commandsResponder(t, commandsrpc.DashboardReply{})
	cmd := projection.Command{
		Name: "hello", Aliases: []string{"hey"}, Response: "Hi!", IsActive: false, StreamOnlineOnly: true,
		Perm: "vip", Cooldown: 30, UserCooldown: 5, AllowedUserID: "77", BumpCounter: "hits", Uses: 9,
	}

	require.NoError(t, NewCommandsRPC(nc, "test.commands").Upsert(t.Context(), "100", cmd))

	assert.Equal(t, commandsrpc.DashboardRequest{
		UserID: "100", Name: "hello", Aliases: []string{"hey"}, Response: "Hi!", IsActive: false, StreamOnlineOnly: true,
		Perm: "vip", Cooldown: 30, UserCooldown: 5, AllowedUserID: "77", BumpCounter: "hits",
	}, <-got)
}

func TestCommandsRPCUpsertReturnsTheServiceError(t *testing.T) {
	nc, _ := commandsResponder(t, commandsrpc.DashboardReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid perm")})

	err := NewCommandsRPC(nc, "test.commands").Upsert(t.Context(), "100", projection.Command{Name: "hello", Response: "Hi!", Perm: "everyone"})

	assert.ErrorContains(t, err, "invalid perm")
}
