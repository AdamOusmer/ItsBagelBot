// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func lifecycleAuthority(t *testing.T, refusal rpc.Refusal) *nats.Conn {
	t.Helper()
	nc := testnats.Connect(t)
	require.NoError(t, bus.Serve(bus.RPCWiring{NC: nc, Log: zap.NewNop(), Queue: "users"}, usersrpc.LifecycleSubject, func(context.Context, usersrpc.LifecycleRequest) usersrpc.LifecycleReply {
		return usersrpc.LifecycleReply{Refusal: refusal}
	}))
	require.NoError(t, nc.Flush())
	return nc
}

func authzMessage(t *testing.T) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(authzUser{UserID: "749558146", EventID: "evt-1", ObservedAt: time.Now().UTC()})
	require.NoError(t, err)
	return &bus.Message{Payload: body}
}

func TestAuthzEventForAccountNotYetCreatedIsAcked(t *testing.T) {
	w := &Worker{botID: "bot", log: zap.NewNop(), lifecycleNC: lifecycleAuthority(t, rpc.Refused(rpc.CodeNotFound, "ent: user not found"))}
	require.NoError(t, w.HandleAuthzGranted(authzMessage(t)))
	require.NoError(t, w.HandleAuthzRevoked(authzMessage(t)))
}

func TestAuthzEventRetriesWhenLifecycleAuthorityFails(t *testing.T) {
	w := &Worker{botID: "bot", log: zap.NewNop(), lifecycleNC: lifecycleAuthority(t, rpc.Refused(rpc.CodeInternal, "database unavailable"))}
	require.Error(t, w.HandleAuthzGranted(authzMessage(t)))
	require.Error(t, w.HandleAuthzRevoked(authzMessage(t)))
}
