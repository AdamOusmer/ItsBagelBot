// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestIdentitySyncPublishesRenameOnlyWhenLoginChanged(t *testing.T) {
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	nc := testnats.Connect(t)
	require.NoError(t, repo.Register(context.Background(), 1001, "oldname", "OldName", "old@example.com"))

	const prefix, invalidation = "test.dashboard", "test.invalidate"
	require.NoError(t, rpc.SubscribeDashboard(rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()},
		Repo:      repo,
	}, prefix, invalidation))

	renames := make(chan invalidate.DTO, 4)
	sub, err := nc.Subscribe(invalidation+".rename", func(m *nats.Msg) {
		var dto invalidate.DTO
		if codec.Unmarshal(m.Data, &dto) == nil {
			renames <- dto
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	require.NoError(t, nc.Flush())

	sync := func(login, display string) usersrpc.IdentitySyncReply {
		reply, err := bus.RequestJSON[usersrpc.IdentitySyncReply](context.Background(), nc, prefix+".identity_sync",
			usersrpc.IdentitySyncRequest{UserID: "1001", Username: login, DisplayName: display})
		require.NoError(t, err)
		return reply
	}

	reply := sync("NewName", "NewName")
	require.True(t, reply.Renamed)
	require.Equal(t, "oldname", reply.PriorUsername)
	select {
	case dto := <-renames:
		require.Equal(t, invalidate.DTO{BroadcasterID: "1001", Keys: []string{"newname", "oldname"}}, dto)
	case <-time.After(3 * time.Second):
		t.Fatal("no rename invalidation after a login change")
	}

	require.False(t, sync("newname", "NewName").Renamed)
	reply = sync("newname", "NEWNAME")
	require.False(t, reply.Renamed, "a casing-only change keeps the login")
	require.NoError(t, nc.Flush())
	select {
	case dto := <-renames:
		t.Fatalf("unexpected rename invalidation %+v", dto)
	case <-time.After(200 * time.Millisecond):
	}
}
