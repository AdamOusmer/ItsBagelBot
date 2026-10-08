// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package commands_test

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/commands"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const settle = 5 * time.Second

type outgressBus struct {
	url string
	js  nats.JetStreamContext
}

func startOutgressBus(t *testing.T) outgressBus {
	t.Helper()
	t.Setenv("NATS_LEAF_URL", "")
	t.Setenv("NATS_HUB_URL", "")
	t.Setenv("NATS_JS_DOMAIN", "hub")
	s, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, JetStreamDomain: "hub",
		StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	require.NoError(t, err)
	s.Start()
	t.Cleanup(s.Shutdown)
	require.True(t, s.ReadyForConnections(settle))
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream(nats.Domain("hub"))
	require.NoError(t, err)
	_, err = js.AddStream(&nats.StreamConfig{
		Name: bus.DiscordOutgressStream.Name, Subjects: bus.DiscordOutgressStream.Subjects,
		Retention: nats.WorkQueuePolicy, Storage: nats.MemoryStorage,
	})
	require.NoError(t, err)
	return outgressBus{url: s.ClientURL(), js: js}
}

func (b outgressBus) publish(t *testing.T, lane string, payload []byte) {
	t.Helper()
	_, err := b.js.Publish(lane, payload)
	require.NoError(t, err)
}

func (b outgressBus) stored() uint64 {
	info, err := b.js.StreamInfo(bus.DiscordOutgressStream.Name)
	if err != nil {
		return math.MaxUint64
	}
	return info.State.Msgs
}

func (b outgressBus) delivered() bool {
	for info := range b.js.ConsumersInfo(bus.DiscordOutgressStream.Name) {
		if info.NumPending > 0 {
			return false
		}
	}
	return true
}

func (b outgressBus) run(t *testing.T, h *handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &commands.Consumer{NATSURL: b.url, Log: zap.NewNop(), Handle: h.handle}
	lanes, err := c.Run(ctx)
	require.NoError(t, err)
	t.Cleanup(lanes.Close)
}

type handler struct {
	err  error
	gate chan struct{}

	mu    sync.Mutex
	order []string
}

func (h *handler) handle(_ context.Context, cmd ddiscord.Command) error {
	h.mu.Lock()
	h.order = append(h.order, cmd.Type)
	first := len(h.order) == 1
	h.mu.Unlock()
	if first && h.gate != nil {
		<-h.gate
	}
	return h.err
}

func (h *handler) handled() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.order...)
}

func command(t *testing.T, kind string) []byte {
	t.Helper()
	return encode(t, ddiscord.Command{Type: kind})
}

func TestConsumerResolvesEveryDelivery(t *testing.T) {
	cases := []struct {
		name        string
		payload     []byte
		handlerErr  error
		wantHandled int
		wantStored  uint64
	}{{
		name:        "acks a handled command",
		payload:     command(t, ddiscord.TypePostChat),
		wantHandled: 1,
	}, {
		name:    "acks and drops a command it cannot decode",
		payload: []byte("not a command"),
	}, {
		name:        "nacks a failed command so the lane redelivers it",
		payload:     command(t, ddiscord.TypePostChat),
		handlerErr:  errBoom,
		wantHandled: 2,
		wantStored:  1,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := startOutgressBus(t)
			h := &handler{err: tc.handlerErr}
			b.publish(t, ddiscord.LaneDefault, tc.payload)

			b.run(t, h)

			require.Eventually(t, func() bool {
				return len(h.handled()) >= tc.wantHandled && b.stored() == tc.wantStored
			}, settle, 10*time.Millisecond)
			require.Len(t, h.handled(), tc.wantHandled)
		})
	}
}

func TestConsumerServesTheModLaneAheadOfADefaultBacklog(t *testing.T) {
	b := startOutgressBus(t)
	h := &handler{gate: make(chan struct{})}
	b.publish(t, ddiscord.LaneDefault, command(t, ddiscord.TypePostChat))
	b.run(t, h)
	require.Eventually(t, func() bool { return len(h.handled()) == 1 }, settle, 10*time.Millisecond)

	for range 3 {
		b.publish(t, ddiscord.LaneDefault, command(t, ddiscord.TypePostChat))
	}
	b.publish(t, ddiscord.LaneMod, command(t, ddiscord.TypeBanMember))
	require.Eventually(t, b.delivered, settle, 10*time.Millisecond)
	close(h.gate)

	require.Eventually(t, func() bool { return len(h.handled()) == 5 }, settle, 10*time.Millisecond)
	require.Equal(t, []string{
		ddiscord.TypePostChat, ddiscord.TypeBanMember,
		ddiscord.TypePostChat, ddiscord.TypePostChat, ddiscord.TypePostChat,
	}, h.handled())
}
