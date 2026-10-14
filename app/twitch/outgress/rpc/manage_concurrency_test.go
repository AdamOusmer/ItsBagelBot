// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/channels"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func subscribedManage(t *testing.T, verify VerifyFunc) (*bus.RPCWiring, *channels.Registry) {
	t.Helper()
	nc := testnats.Connect(t)
	registry := channels.New(valkeytest.Spawn(t))
	t.Cleanup(registry.Close)
	registry.SeedPause(false)
	wiring := bus.RPCWiring{NC: nc, Queue: "outgress", Log: zap.NewNop()}
	require.NoError(t, SubscribeManage(SubscriptionConfig{RPCWiring: wiring, Prefix: "test.outgress", Verify: verify}, registry, nil))
	return &wiring, registry
}

func TestSlowVerifyDoesNotBlockOtherChannelReads(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	wiring, registry := subscribedManage(t, func(ctx context.Context, _ string, _ bool) (manage.Verification, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return manage.Verification{}, nil
	})
	require.NoError(t, registry.SetSubState(t.Context(), "7", "ok", ""))

	go func() {
		_, _ = bus.RequestJSONTimeout[manage.ChannelReply](context.Background(), wiring.NC, "test.outgress.channel.get", manage.ChannelRequest{BroadcasterID: "42", Verify: true}, 5*time.Second)
	}()
	time.Sleep(100 * time.Millisecond)

	reply, err := bus.RequestJSONTimeout[manage.ChannelReply](t.Context(), wiring.NC, "test.outgress.channel.get", manage.ChannelRequest{BroadcasterID: "7"}, time.Second)

	require.NoError(t, err)
	require.True(t, reply.Found)
	require.Equal(t, "ok", reply.Channel.SubState)
}

func TestChannelSetKeepsSubStateWrittenElsewhere(t *testing.T) {
	wiring, registry := subscribedManage(t, nil)
	require.NoError(t, registry.SetSubState(t.Context(), "42", "failing", "missing: channel.cheer"))
	isMod := true

	reply, err := bus.RequestJSONTimeout[manage.ChannelReply](t.Context(), wiring.NC, "test.outgress.channel.set", manage.ChannelRequest{BroadcasterID: "42", IsMod: &isMod}, time.Second)

	require.NoError(t, err)
	require.True(t, reply.Channel.IsMod)
	require.True(t, reply.Channel.Enabled)
	require.Equal(t, "failing", reply.Channel.SubState)
	require.Equal(t, "missing: channel.cheer", reply.Channel.SubError)
}
