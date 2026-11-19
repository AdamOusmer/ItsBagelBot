// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package channels

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func registryReplicas(t *testing.T, client valkey.Client) (*Registry, *Registry) {
	t.Helper()
	nc := testnats.Connect(t)
	peerNC, err := nats.Connect(nc.ConnectedUrl())
	require.NoError(t, err)
	t.Cleanup(peerNC.Close)
	writer := New(client, bus.NewInvalidations(nc, "cache.invalidate", nil))
	peer := New(client, bus.NewInvalidations(peerNC, "cache.invalidate", nil))
	t.Cleanup(writer.Close)
	t.Cleanup(peer.Close)
	require.NoError(t, nc.Flush())
	require.NoError(t, peerNC.Flush())
	return writer, peer
}

func channelFake(t *testing.T) *valkeytest.Server {
	t.Helper()
	server := valkeytest.New(t)
	for _, cmd := range []string{"HSETNX", "HSET", "SADD"} {
		server.Handle(cmd, func(*valkeytest.Server, valkeytest.Args) valkeytest.Reply { return valkeytest.Int(1) })
	}
	server.Handle("HGETALL", func(*valkeytest.Server, valkeytest.Args) valkeytest.Reply {
		return valkeytest.Flat([]string{"enabled", "1"})
	})
	return server
}

func TestChannelWriteInvalidatesReplicasDespiteRetentionFailure(t *testing.T) {
	server := channelFake(t)
	writer, peer := registryReplicas(t, server.Client())
	for _, r := range []*Registry{writer, peer} {
		r.Prime(manage.Channel{BroadcasterID: "42"})
	}
	server.Fail(valkeytest.Failure{Cmd: "EVAL", Message: "retention unavailable"})
	err := writer.SetFlags(t.Context(), "42", &enabled, nil)
	require.ErrorContains(t, err, "retention unavailable")
	require.True(t, registryEnabled(t, writer))
	require.Eventually(t, func() bool { return registryEnabled(t, peer) }, time.Second, time.Millisecond)
}

func registryEnabled(t *testing.T, r *Registry) bool {
	t.Helper()
	ch, found, err := r.Get(t.Context(), "42")
	require.NoError(t, err)
	return found && ch.Enabled
}

func TestEraseIncarnationInvalidatesPeerRegistry(t *testing.T) {
	client := channelsRealValkey(t)
	seed := New(client)
	t.Cleanup(seed.Close)
	require.NoError(t, seed.SetFlags(t.Context(), "42", &enabled, nil))
	require.NoError(t, seed.SetIncarnation(t.Context(), "42", 100))
	writer, peer := registryReplicas(t, client)
	require.True(t, registryEnabled(t, peer))

	err := writer.EraseIncarnation(t.Context(), "42", 200)
	require.NoError(t, err)
	require.True(t, registryEnabled(t, peer))
	err = writer.EraseIncarnation(t.Context(), "42", 100)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, found, err := peer.Get(t.Context(), "42")
		require.NoError(t, err)
		return !found
	}, time.Second, time.Millisecond)
}

func TestChannelInvalidationSkipsMalformedIDs(t *testing.T) {
	for _, id := range []string{"42", "invalid", "18446744073709551616"} {
		t.Run(id, func(t *testing.T) {
			server := channelFake(t)
			server.Handle("EVAL", func(*valkeytest.Server, valkeytest.Args) valkeytest.Reply { return valkeytest.Int(1) })
			hub := bus.NewInvalidations(nil, "", nil)
			r := New(server.Client(), hub)
			t.Cleanup(r.Close)
			core, logs := observer.New(zap.WarnLevel)
			r.log = zap.New(core)
			var events []cache.Event
			hub.Watch(invalidate.ScopeOutgress, func(e cache.Event) { events = append(events, e) })
			r.Prime(manage.Channel{BroadcasterID: id})
			require.NoError(t, r.SetFlags(context.Background(), id, &enabled, nil))
			ch, _, err := r.Get(t.Context(), id)
			require.NoError(t, err)
			require.True(t, ch.Enabled)
			if id == "42" {
				require.Equal(t, []cache.Event{{Scope: invalidate.ScopeOutgress, ID: 42}}, events)
				require.Zero(t, logs.Len())
				return
			}
			require.Empty(t, events)
			require.Equal(t, 1, logs.Len())
		})
	}
}
