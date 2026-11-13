// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/admin/ent"
	"ItsBagelBot/app/db/admin/ent/adminuser"
	"ItsBagelBot/app/db/admin/ent/enttest"
	"ItsBagelBot/app/db/admin/repository"
	"ItsBagelBot/app/db/admin/rpc"
	"ItsBagelBot/internal/analytics"
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

const (
	authPrefix           = "admin.auth"
	auditPrefix          = "admin.audit"
	analyticsSubject     = "admin.analytics"
	activeSubject        = "admin.staff.active"
	auditDeliverySubject = "admin.audit.delivery"
	absentTarget         = "999999"
)

type harness struct {
	nc     *nats.Conn
	client *ent.Client
}

func newHarness(t *testing.T, readers ...*analytics.Reader) harness {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	nc := testnats.Connect(t)
	w := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repository.New(client)}
	if len(readers) > 0 {
		w.Analytics = readers[0]
	}
	require.NoError(t, rpc.SubscribeAdminAuth(w, client, authPrefix, auditPrefix))
	for _, binding := range []struct {
		subject   string
		subscribe func(rpc.Wiring, *ent.Client, string) error
	}{
		{analyticsSubject, rpc.SubscribeAnalytics},
		{activeSubject, rpc.SubscribeActiveStaff},
		{auditDeliverySubject, rpc.SubscribeAuditDelivery},
	} {
		require.NoError(t, binding.subscribe(w, client, binding.subject))
	}
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

func (h harness) auth(t *testing.T, verb string, req usersrpc.AuthRequest) usersrpc.AuthReply {
	t.Helper()
	prefix := authPrefix
	if rest, audit := strings.CutPrefix(verb, "audit:"); audit {
		prefix, verb = auditPrefix, rest
	}
	return call[usersrpc.AuthReply](t, h, prefix+"."+verb, req)
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
