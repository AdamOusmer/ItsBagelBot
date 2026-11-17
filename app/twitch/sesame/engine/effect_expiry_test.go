// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExpiredReceiptRetryResolvesPipelineCompletion(t *testing.T) {
	store := &observedLeaseStore{recordingStore: newRecordingStore()}
	store.failCompletion.Store(true)
	d := NewEventDedup(store, "", time.Hour, nil)
	m := module.NewModule("money", module.KindCore)
	m.On(chatType, func(ctx context.Context, c *module.Context, _ module.Emit) error {
		return d.Effect(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: "money"}, func(context.Context) error { return nil })
	})
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Dedup: d}, NewRegistry(zap.NewNop(), m.Build()), Config{})
	t.Cleanup(p.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	msg := commandMsg(t, "retry", "hello")
	msg.SetContext(ctx)
	require.NoError(t, p.Process(msg))
	require.Eventually(t, func() bool { return store.attempts.Load() > 0 }, time.Second, time.Millisecond)
	cancel()
	finish, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	p.Finish(finish)
	require.NoError(t, finish.Err())
	require.Zero(t, p.completions.remaining())
}
