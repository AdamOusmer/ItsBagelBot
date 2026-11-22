// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type poweredFixture struct {
	nc    *nats.Conn
	store *projection.Store
	hub   *bus.Invalidations
	owner *atomic.Int32
}

func newPoweredFixture(t *testing.T, owned map[string]*overlaysrpc.PoweredOverlay) poweredFixture {
	t.Helper()
	f := poweredFixture{
		nc:    bustest.NATS(t),
		store: projection.NewStore(valkeytest.Spawn(t)),
		hub:   bus.NewInvalidations(nil, "", zap.NewNop()),
		owner: &atomic.Int32{},
	}
	sub, err := f.nc.Subscribe(overlaysrpc.OwnerPoweredByTokenSubject, func(m *nats.Msg) {
		f.owner.Add(1)
		var req overlaysrpc.OwnerPoweredByTokenRequest
		_ = codec.Unmarshal(m.Data, &req)
		body, _ := codec.Marshal(ownerReply(owned, req.Token))
		_ = m.Respond(body)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	require.NoError(t, SubscribePowered(bus.RPCWiring{NC: f.nc, Queue: "projector-rpc", Log: zap.NewNop()}, PoweredConfig{Store: f.store, Invalidations: f.hub}))
	return f
}

func ownerReply(owned map[string]*overlaysrpc.PoweredOverlay, token string) overlaysrpc.OwnerPoweredReply {
	if overlay, ok := owned[token]; ok {
		return overlaysrpc.OwnerPoweredReply{Overlay: overlay}
	}
	return overlaysrpc.OwnerPoweredReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "overlay not found")}
}

func (f poweredFixture) resolve(t *testing.T, token string) overlaysrpc.PoweredResolveReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var reply overlaysrpc.PoweredResolveReply
	request := bus.RPCRequest{Subject: overlaysrpc.PoweredResolveSubject, Body: overlaysrpc.PoweredResolveRequest{Token: token}, DecodeErrorReply: true}
	require.NoError(t, bus.RequestInto(ctx, f.nc, request, &reply))
	return reply
}

func poweredLink(userID uint64, token string, everyMinutes int) *overlaysrpc.PoweredOverlay {
	config := overlaysrpc.DefaultPoweredConfig()
	config.EveryMinutes = everyMinutes
	return &overlaysrpc.PoweredOverlay{UserID: userID, Token: token, Config: config, Revision: 1}
}

func TestPoweredResolveReadsProjectionThenOwner(t *testing.T) {
	warm := poweredLink(31, "warm", 12)
	cold := poweredLink(32, "cold", 25)
	f := newPoweredFixture(t, map[string]*overlaysrpc.PoweredOverlay{"cold": cold})
	ctx := t.Context()
	_, err := f.store.SetPoweredOverlay(ctx, projection.PoweredWrite{Overlay: *warm})
	require.NoError(t, err)
	require.NoError(t, f.store.SetUser(ctx, 31, projection.UserProjection{Status: "premium", IsActive: true}))

	cases := []struct {
		name       string
		token      string
		want       overlaysrpc.PoweredResolveReply
		ownerCalls int32
	}{
		{"projected premium link", "warm", overlaysrpc.PoweredResolveReply{Config: warm.Config, Premium: true}, 0},
		{"owner fills a cold link", "cold", overlaysrpc.PoweredResolveReply{Config: cold.Config}, 1},
		{"cold link served from cache", "cold", overlaysrpc.PoweredResolveReply{Config: cold.Config}, 1},
		{"unknown link", "missing", overlaysrpc.PoweredResolveReply{Refusal: domainrpc.Refused(domainrpc.CodeNotFound, "overlay not found")}, 2},
		{"missing token", "", overlaysrpc.PoweredResolveReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "token required")}, 2},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, f.resolve(t, c.token), c.name)
		assert.Equal(t, c.ownerCalls, f.owner.Load(), c.name)
	}

	stored, _, err := f.store.PoweredOverlayByToken(ctx, "cold")
	require.NoError(t, err)
	assert.Equal(t, *cold, stored, "the owner answer is written back to the projection")
}

func TestPoweredResolveRereadsAfterInvalidation(t *testing.T) {
	f := newPoweredFixture(t, nil)
	ctx := t.Context()
	link := poweredLink(33, "live", 15)
	_, err := f.store.SetPoweredOverlay(ctx, projection.PoweredWrite{Overlay: *link})
	require.NoError(t, err)
	assert.Equal(t, 15, f.resolve(t, "live").Config.EveryMinutes)

	link.Revision, link.Config.EveryMinutes = 2, 30
	_, err = f.store.SetPoweredOverlay(ctx, projection.PoweredWrite{Overlay: *link})
	require.NoError(t, err)
	assert.Equal(t, 15, f.resolve(t, "live").Config.EveryMinutes, "the process cache serves until invalidated")

	require.NoError(t, f.hub.Publish(invalidate.ScopeOverlays, 33, "live"))
	assert.Equal(t, 30, f.resolve(t, "live").Config.EveryMinutes)
}
