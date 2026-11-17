// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFailedCommandProjectionNeverInvalidates(t *testing.T) {
	h := bus.NewInvalidations(nil, "", nil)
	invalidated := make(chan cache.Event, 1)
	h.Watch(invalidate.ScopeCommands, func(e cache.Event) { invalidated <- e })
	d := &Dashboard{invalidations: h, log: zap.NewNop(), writeGate: make(chan struct{}, 1)}
	completed := make(chan struct{})
	d.writeAsync(1001, projectionWrite{section: invalidate.ScopeCommands, failure: "failed", set: func(context.Context) error {
		close(completed)
		return errors.New("write failed")
	}})
	<-completed
	d.writeGate <- struct{}{}
	<-d.writeGate
	select {
	case <-invalidated:
		t.Fatal("a failed write evicted the committed projection")
	default:
	}
}

func TestStatusCacheEvictsOnSharedInvalidation(t *testing.T) {
	nc := bustest.NATS(t)
	store := projection.NewStore(moduleTestValkey(t))
	h := bus.NewInvalidations(nil, "", nil)
	require.NoError(t, SubscribeStatus(bus.RPCWiring{NC: nc, Queue: "status-content", Log: zap.NewNop()}, StatusConfig{
		Store: store, Subject: "test.status", UsersTopic: "test.users", Invalidations: h,
	}))
	for _, scope := range []string{invalidate.ScopeUserProjection, invalidate.ScopeLive} {
		require.NoError(t, store.SetUser(t.Context(), 1001, projection.UserProjection{Status: "premium", IsActive: true}))
		require.NoError(t, h.Publish(scope, 1001))
		status := requestContentStatus(t, nc)
		require.Equal(t, "premium", status.Tier)
		require.NoError(t, store.SetUser(t.Context(), 1001, projection.UserProjection{Status: "standard", IsActive: true}))
		require.NoError(t, h.Publish(scope, 1001))
		status = requestContentStatus(t, nc)
		require.Equal(t, "standard", status.Tier)
	}
}

func requestContentStatus(t *testing.T, nc *nats.Conn) projectorrpc.StatusReply {
	t.Helper()
	reply, err := bus.RequestJSONTimeout[projectorrpc.StatusReply](t.Context(), nc, "test.status", projectorrpc.StatusRequest{BroadcasterID: "1001"}, time.Second)
	require.NoError(t, err)
	return reply
}
