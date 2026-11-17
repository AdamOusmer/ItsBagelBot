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
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessMetricsNeverAcquireEventLeases(t *testing.T) {
	client := valkeytest.Spawn(t)
	store := newRecordingStore()
	recorder := analytics.New(client, zap.NewNop(), nil, analytics.WithoutTicker())
	t.Cleanup(recorder.Close)
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: recorder, Dedup: NewEventDedup(store, "", time.Hour, nil)}, NewRegistry(zap.NewNop()), Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "metrics", "hello")
	require.NoError(t, p.Process(msg))
	assertPendingCompletion(t, msg)
	require.Empty(t, store.owners)
	require.NoError(t, recorder.Flush(t.Context()))
	awaitCompletion(t, msg, true)
	count, err := client.Do(t.Context(), client.B().Hget().Key(catalog.CounterKey(time.Now())).Field("chat.messages").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestReporterGenerationOutlivesHandlerCutoff(t *testing.T) {
	for _, dialect := range []string{"sqlite3", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			verifier := newCompletionReceipts(t, dialect)
			b := newCompletionBroker(t, 300*time.Millisecond, 3*time.Second)
			client := valkeytest.Spawn(t)
			p, reporter := b.pipeline(t, client)
			t.Cleanup(reporter.Close)
			t.Cleanup(p.Close)
			b.pub.failAll.Store(true)
			b.publish(t)
			msg := b.receive(t)
			require.NoError(t, p.Process(msg))
			p.dedup.Finish(t.Context())
			select {
			case <-b.messages:
				t.Fatal("generation delivery was redelivered")
			case <-time.After(13 * time.Second):
			}
			assertPendingCompletion(t, msg)
			require.False(t, bus.DeliveryAlive(msg.Context()))
			info, err := b.js.StreamInfo(bus.TwitchIngressPremiumStream.Name)
			require.NoError(t, err)
			require.Zero(t, info.State.Msgs)
			owned, err := client.Do(t.Context(), client.B().Exists().Key("completion:twitch-chat:earn").Build()).AsInt64()
			require.NoError(t, err)
			require.Zero(t, owned)
			renewable := p.dedup.pendingEffects()
			require.Empty(t, renewable)
			require.True(t, bus.SubscriberHealthy(b.sub))
			duplicateRan := false
			require.NoError(t, p.dedup.Effect(t.Context(), EffectRef{Identity: "twitch-chat", Effect: EffectEarn}, func(context.Context) error {
				duplicateRan = true
				return nil
			}))
			require.False(t, duplicateRan)
			b.pub.failAll.Store(false)
			p.Finish(t.Context())
			assertPendingCompletion(t, msg)
			verifier.replay(t, b.js)
		})
	}
}

func TestDeferredExecutionRetainsDeliveryBeforeItsFirstGeneration(t *testing.T) {
	b := newCompletionBroker(t, 100*time.Millisecond)
	release := make(chan struct{})
	m := module.NewModule("deferred", module.KindCore)
	m.On(chatType, func(ctx context.Context, _ *module.Context, _ module.Emit) error {
		go idempotency.Async(ctx, func(context.Context) error { <-release; return nil })()
		return nil
	})
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: b.pub, Log: zap.NewNop(), Seq: NewSequencer()}, NewRegistry(zap.NewNop(), m.Build()), Config{})
	t.Cleanup(p.Close)
	b.publish(t)
	msg := b.receive(t)
	require.NoError(t, p.Process(msg))
	select {
	case <-b.messages:
		t.Fatal("deferred execution was redelivered")
	case <-time.After(600 * time.Millisecond):
	}
	close(release)
	awaitCompletion(t, msg, true)
}
