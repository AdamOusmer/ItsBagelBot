// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

var errValkeyUnreachable = errors.New("valkey unreachable")

type unreachableValkey struct{ valkey.Client }

func (unreachableValkey) Do(context.Context, valkey.Completed) valkey.ValkeyResult {
	return valkey.NewResult(valkey.ValkeyMessage{}, errValkeyUnreachable)
}

func (unreachableValkey) DoMulti(_ context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	out := make([]valkey.ValkeyResult, len(cmds))
	for i := range out {
		out[i] = valkey.NewResult(valkey.ValkeyMessage{}, errValkeyUnreachable)
	}
	return out
}

func refusingValkeyAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go acceptRefusing(ln)
	return ln.Addr().String()
}

func acceptRefusing(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go refuseEveryRead(conn)
	}
}

func refuseEveryRead(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 512)
	for {
		if _, err := conn.Read(buf); err != nil {
			return
		}
		if _, err := conn.Write([]byte("-ERR unknown command 'HELLO'\r\n")); err != nil {
			return
		}
	}
}

func statusTestRPC(t *testing.T, replies ...string) (*statusRPC, *atomic.Int32) {
	t.Helper()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{refusingValkeyAddr(t)},
		AlwaysRESP2:       true,
		DisableCache:      true,
		ForceSingleClient: true,
		ClientSetInfo:     valkey.DisableClientSetInfo,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	nc := bustest.NATS(t)
	served := bustest.Respond(t, nc, "test.users", replies...)
	s := &statusRPC{
		valkey:     projection.NewStore(unreachableValkey{client}),
		views:      cache.New[statusEntry](statusCacheCapacity, statusCacheTTL),
		nc:         nc,
		usersTopic: "test.users",
		log:        zap.NewNop(),
	}
	t.Cleanup(s.views.Close)
	return s, served
}

func TestStatusReportsUserLookupFailureWithoutCachingIt(t *testing.T) {
	s, served := statusTestRPC(t, `{"error":"request failed","code":"internal"}`, `{"user_id":"7","status":"premium","is_active":true}`)
	req := projectorrpc.StatusRequest{BroadcasterID: "7"}
	ctx := context.Background()

	reply := s.handleGet(ctx, req)
	require.Equal(t, "standard", reply.Tier)
	require.NotEmpty(t, reply.Error, "ingress holds an error reply for seconds, a standard tier for its full TTL")

	reply = s.handleGet(ctx, req)
	require.Empty(t, reply.Error)
	require.Equal(t, "premium", reply.Tier, "the failed lookup must not be cached")
	require.EqualValues(t, 2, served.Load())
}

func TestStatusCachesAbsentAccountAsStandard(t *testing.T) {
	s, served := statusTestRPC(t, `{"user_id":"7","error":"user account not found","code":"not_found"}`)
	req := projectorrpc.StatusRequest{BroadcasterID: "7"}

	for range 2 {
		reply := s.handleGet(context.Background(), req)
		require.Empty(t, reply.Error)
		require.Equal(t, "standard", reply.Tier)
	}
	require.EqualValues(t, 1, served.Load())
}
