// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func ownershipPipeline(t *testing.T, client valkey.Client, store idempotency.Store) *Pipeline {
	t.Helper()
	rec := analytics.New(client, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(rec.Close)
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(),
		Analytics: rec, Dedup: NewEventDedup(store, "ownership:", 2*time.Minute, nil)}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), silentCore()), Config{})
	t.Cleanup(p.Close)
	return p
}

func TestProcessTwoPodsRetainMetricsWithoutEventReceipts(t *testing.T) {
	for _, mode := range []string{"upstream-copy", "transient"} {
		t.Run(mode, func(t *testing.T) { testTwoPodOwnership(t, mode) })
	}
}

func testTwoPodOwnership(t *testing.T, mode string) {
	client := valkeytest.Spawn(t)
	store := idempotency.NewValkeyStore(client, "ownership:", nil)
	p := ownershipPipeline(t, client, store)
	peer := ownershipPipeline(t, client, idempotency.NewValkeyStore(client, "ownership:", nil))
	key := catalog.CounterKey(time.Now())
	broken := mode == "transient"
	if broken {
		require.NoError(t, client.Do(t.Context(), client.B().Set().Key(key).Value("wrong type").Build()).Error())
	}
	first := commandMsg(t, "two-pods", "hello")
	require.NoError(t, p.Process(first))
	prepareOwnershipFailure(t, p, mode)
	copy := commandMsg(t, "two-pods", "hello")
	require.NoError(t, peer.Process(copy))
	assertPendingCompletion(t, copy)
	assertPendingCompletion(t, first)
	if broken {
		require.NoError(t, client.Do(t.Context(), client.B().Del().Key(key).Build()).Error())
	}
	p.Finish(t.Context())
	awaitCompletion(t, first, true)
	peer.Finish(t.Context())
	awaitCompletion(t, copy, true)
	retry := commandMsg(t, "two-pods", "hello")
	require.NoError(t, peer.Process(retry))
	peer.Finish(t.Context())
	awaitCompletion(t, retry, true)
	count, err := client.Do(t.Context(), client.B().Hget().Key(key).Field("chat.messages").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
}

func prepareOwnershipFailure(t *testing.T, p *Pipeline, mode string) {
	t.Helper()
	switch mode {
	case "transient":
		require.Error(t, p.analytics.Flush(t.Context()))
	}
}

func TestProcessPanicReleasesTheUnfinishedEffect(t *testing.T) {
	client := valkeytest.Spawn(t)
	dedup := NewEventDedup(idempotency.NewValkeyStore(client, "panic:", nil), "panic:", time.Minute, nil)
	panicking := true
	mutations := 0
	m := module.NewModule("panic", module.KindCore)
	m.On(chatType, func(ctx context.Context, c *module.Context, _ module.Emit) error {
		return dedup.Effect(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: "mutation"}, func(context.Context) error {
			if panicking {
				panic("injected panic")
			}
			mutations++
			return nil
		})
	})
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Seq: NewSequencer(), Dedup: dedup}
	d.Analytics = analytics.New(client, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(d.Analytics.Close)
	p := NewPipeline(d, NewRegistry(zap.NewNop(), m.Build()), Config{})
	t.Cleanup(p.Close)
	first := commandMsg(t, "panic", "hello")
	require.Panics(t, func() { _ = p.Process(first) })
	p.Finish(t.Context())
	awaitCompletion(t, first, false)
	panicking = false
	retry := commandMsg(t, "panic", "hello")
	require.NoError(t, p.Process(retry))
	p.Finish(t.Context())
	awaitCompletion(t, retry, true)
	require.Equal(t, 1, mutations)
}
