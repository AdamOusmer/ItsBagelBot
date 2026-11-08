// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package rpc

import (
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCountdownRPC(t *testing.T) {
	h := newStateHarness(t)
	req := modulesrpc.CountdownRequest{UserID: "2", Name: "Timer", Seconds: 10, OperationID: "one"}
	var start modulesrpc.CountdownReply
	h.call(t, "countdown.start", req, &start)
	require.True(t, start.Found)
	require.Empty(t, start.Error)
	var same modulesrpc.CountdownReply
	h.call(t, "countdown.start", req, &same)
	require.Equal(t, start.ExpiresAtMS, same.ExpiresAtMS)
	var peek modulesrpc.CountdownReply
	h.call(t, "countdown.peek", modulesrpc.CountdownRequest{UserID: "2", Name: "timer"}, &peek)
	require.Equal(t, start.ExpiresAtMS, peek.ExpiresAtMS)
	var none modulesrpc.CountdownDeliveryReply
	h.call(t, "countdown.claim", modulesrpc.CountdownDeliveryRequest{}, &none)
	require.False(t, none.Found)
	require.Empty(t, none.Error)
}

func TestCountdownProgressRPCRejectsInvalidLease(t *testing.T) {
	h := newStateHarness(t)
	var reply modulesrpc.CountdownDeliveryReply
	h.call(t, "countdown.progress", modulesrpc.CountdownProgressRequest{Delivery: modulesrpc.CountdownDelivery{UserID: "2", Name: "timer", Generation: "gen", Token: "stale", ExpiresAtMS: 123}, Key: "recipe", Status: "completed"}, &reply)
	require.False(t, reply.Found)
	require.Empty(t, reply.Error)
}
