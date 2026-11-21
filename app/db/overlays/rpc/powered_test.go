// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/overlays/ent"
	"ItsBagelBot/app/db/overlays/ent/enttest"
	"ItsBagelBot/app/db/overlays/repository"
	"ItsBagelBot/app/db/overlays/rpc"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func serve(t *testing.T) *nats.Conn {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	nc := bustest.NATS(t)
	w := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "test"},
		Repo:      repository.New(client, bustest.NewPublisher()),
	}
	require.NoError(t, rpc.SubscribeDashboard(w))
	require.NoError(t, rpc.SubscribeOwner(w))
	require.NoError(t, nc.Flush())
	return nc
}

func request[Rep any](t *testing.T, nc *nats.Conn, subject string, req any) Rep {
	t.Helper()
	body, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := nc.Request(subject, body, time.Second)
	require.NoError(t, err)
	var reply Rep
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply
}

func TestDashboardRefusals(t *testing.T) {
	nc := serve(t)
	view := request[overlaysrpc.PoweredView](t, nc, overlaysrpc.PoweredGetSubject, overlaysrpc.PoweredGetRequest{UserID: "1001"})
	require.Equal(t, domainrpc.CodeOK, view.Code)

	valid := overlaysrpc.DefaultPoweredConfig()
	invalid := valid
	invalid.Directions = nil

	cases := []struct {
		name    string
		subject string
		req     any
		want    domainrpc.Code
	}{
		{"missing user", overlaysrpc.PoweredGetSubject, overlaysrpc.PoweredGetRequest{}, domainrpc.CodeInvalid},
		{"invalid config", overlaysrpc.PoweredSaveSubject, overlaysrpc.PoweredSaveRequest{UserID: "1001", Config: invalid, Revision: view.Revision}, domainrpc.CodeInvalid},
		{"stale revision", overlaysrpc.PoweredSaveSubject, overlaysrpc.PoweredSaveRequest{UserID: "1001", Config: valid, Revision: view.Revision + 5}, domainrpc.CodeConflict},
		{"saved", overlaysrpc.PoweredSaveSubject, overlaysrpc.PoweredSaveRequest{UserID: "1001", Config: valid, Revision: view.Revision}, domainrpc.CodeOK},
		{"rotated", overlaysrpc.PoweredRotateSubject, overlaysrpc.PoweredRotateRequest{UserID: "1001"}, domainrpc.CodeOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := request[overlaysrpc.PoweredView](t, nc, tc.subject, tc.req)
			require.Equal(t, tc.want, reply.Code, reply.Error)
		})
	}
}

func TestOwnerLookupsFollowTheCurrentLink(t *testing.T) {
	nc := serve(t)
	created := request[overlaysrpc.PoweredView](t, nc, overlaysrpc.PoweredGetSubject, overlaysrpc.PoweredGetRequest{UserID: "1001"})
	rotated := request[overlaysrpc.PoweredView](t, nc, overlaysrpc.PoweredRotateSubject, overlaysrpc.PoweredRotateRequest{UserID: "1001"})
	token := func(v overlaysrpc.PoweredView) string { return v.URL[len(overlaysrpc.PoweredURLPrefix):] }

	cases := []struct {
		name    string
		subject string
		req     any
		want    domainrpc.Code
	}{
		{"unknown token", overlaysrpc.OwnerPoweredByTokenSubject, overlaysrpc.OwnerPoweredByTokenRequest{Token: "missing"}, domainrpc.CodeNotFound},
		{"current token", overlaysrpc.OwnerPoweredByTokenSubject, overlaysrpc.OwnerPoweredByTokenRequest{Token: token(rotated)}, domainrpc.CodeOK},
		{"replaced token", overlaysrpc.OwnerPoweredByTokenSubject, overlaysrpc.OwnerPoweredByTokenRequest{Token: token(created)}, domainrpc.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply := request[overlaysrpc.OwnerPoweredReply](t, nc, tc.subject, tc.req)
			require.Equal(t, tc.want, reply.Code, reply.Error)
			require.Equal(t, tc.want == domainrpc.CodeOK, reply.Overlay != nil)
		})
	}
}
