// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDeliveryRedeliversPastLiveLeaseWithinRetention(t *testing.T) {
	b := newCompletionBroker(t)
	client := valkeytest.Spawn(t)
	first, peer := recoveryDedup(t, client), recoveryDedup(t, client)
	b.publish(t)
	msg := b.receive(t)
	ref := EffectRef{Identity: msg.UUID, Effect: "money"}
	ctx, work := idempotency.NewWork(msg.WithDeliveryContext(msg.Context()))
	abandoned := &idempotency.Handoff{}
	require.NoError(t, first.Effect(ctx, ref, func(ctx context.Context) error { idempotency.Join(ctx, abandoned); return nil }))
	work.Seal(nil)
	started := time.Now()
	msg.Nack()
	applied := 0
	for {
		retry := b.receive(t)
		retryCtx, retryWork := idempotency.NewWork(retry.WithDeliveryContext(retry.Context()))
		completion := retry.DeferCompletion()
		retryWork.Watch(func(r idempotency.Result) { completion.Complete(r.Err) })
		err := peer.Effect(retryCtx, ref, func(context.Context) error { applied++; return nil })
		retryWork.Seal(err)
		if err == nil {
			awaitCompletion(t, retry, true)
			break
		}
		require.ErrorAs(t, err, new(effectBusy))
		awaitCompletion(t, retry, false)
		require.Less(t, time.Since(started), 30*time.Second)
	}
	require.Equal(t, 1, applied)
	require.Less(t, time.Since(started), 30*time.Second)
	abandoned.Resolve(nil)
}

func TestProcessDoesNotStartAnEffectWhosePreparedLeaseWasLost(t *testing.T) {
	client := valkeytest.Spawn(t)
	d := recoveryDedup(t, client)
	started, release := make(chan struct{}), make(chan struct{})
	first := module.NewModule("first", module.KindCore).ProtectReplay()
	first.On(chatType, func(ctx context.Context, _ *module.Context, _ module.Emit) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	second := module.NewModule("second", module.KindCore).ProtectReplay()
	called := false
	second.On(chatType, func(context.Context, *module.Context, module.Emit) error { called = true; return nil })
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Dedup: d}, NewRegistry(zap.NewNop(), first.Build(), second.Build()), Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "not-started", "hello")
	done := make(chan error, 1)
	go func() { done <- p.Process(msg) }()
	<-started
	require.NoError(t, client.Do(t.Context(), client.B().Set().Key("recovery:not-started:handler:second").Value("peer").Build()).Error())
	require.Eventually(t, func() bool { return d.LeaseConflicts() > 0 }, 5*time.Second, 10*time.Millisecond)
	close(release)
	require.True(t, errors.As(<-done, new(effectBusy)))
	awaitCompletion(t, msg, false)
	require.False(t, called)
}
