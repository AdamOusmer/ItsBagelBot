// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	adminPrefix  = "admin.user"
	internalGet  = "admin.internal.get"
	authPrefix   = "admin.auth"
	auditPrefix  = "admin.audit"
	absentTarget = "999999"
)

type harness struct {
	nc     *nats.Conn
	client *ent.Client
}

func newHarness(t *testing.T) harness {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	nc := testnats.Connect(t)
	wiring := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repo}
	require.NoError(t, rpc.SubscribeAdmin(wiring, client, rpc.AdminConfig{
		Prefix: adminPrefix, InternalGetSubject: internalGet, InvalidationPrefix: "admin.invalidate",
	}))
	require.NoError(t, rpc.SubscribeAdminAuth(wiring, client, authPrefix, auditPrefix))
	return harness{nc: nc, client: client}
}

func call[Reply any](t *testing.T, h harness, subject string, req any) Reply {
	t.Helper()
	payload, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := h.nc.Request(subject, payload, 3*time.Second)
	require.NoError(t, err)
	var reply Reply
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply
}

func (h harness) admin(t *testing.T, verb string, actor uint64, req usersrpc.AdminRequest) usersrpc.AdminReply {
	t.Helper()
	req.ActorID = strconv.FormatUint(actor, 10)
	return call[usersrpc.AdminReply](t, h, adminPrefix+"."+verb, req)
}

func (h harness) auth(t *testing.T, verb string, req usersrpc.AuthRequest) usersrpc.AuthReply {
	t.Helper()
	return call[usersrpc.AuthReply](t, h, authPrefix+"."+verb, req)
}

func (h harness) audit(t *testing.T, verb string, req usersrpc.AuthRequest) usersrpc.AuthReply {
	t.Helper()
	return call[usersrpc.AuthReply](t, h, auditPrefix+"."+verb, req)
}

type staffFixture struct {
	id     uint64
	role   adminuser.Role
	active bool
}

func (h harness) staff(fixtures ...staffFixture) {
	for _, f := range fixtures {
		login := fmt.Sprintf("staff-%d", f.id)
		h.client.AdminUser.Create().SetID(f.id).SetLogin(login).SetDisplayName(login).SetRole(f.role).SetActive(f.active).ExecX(context.Background())
	}
}

func (h harness) users(n int, base time.Time) {
	for i := range n {
		h.client.User.Create().
			SetID(uint64(9000 + i)).
			SetUsername(fmt.Sprintf("user-%02d", i)).
			SetEmail(fmt.Sprintf("user-%02d@example.invalid", i)).
			SetStatus(user.StatusFree).
			SetUpdatedAt(base.Add(time.Duration(i) * time.Minute)).
			ExecX(context.Background())
	}
}

func usernames(reply usersrpc.AdminReply) []string {
	names := make([]string, 0, len(reply.Users))
	for _, u := range reply.Users {
		names = append(names, u.Username)
	}
	return names
}

func userIDs(reply usersrpc.AdminReply) []uint64 {
	ids := make([]uint64, 0, len(reply.Users))
	for _, u := range reply.Users {
		ids = append(ids, u.ID)
	}
	return ids
}
