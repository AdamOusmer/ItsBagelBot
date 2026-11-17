// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type observedLeaseStore struct {
	*recordingStore
	maxBatch       atomic.Int64
	failCompletion atomic.Bool
	attempts       atomic.Int64
}

func (s *observedLeaseStore) UpdateLeases(ctx context.Context, batch idempotency.LeaseBatch) ([]int64, error) {
	if s.failsBatch(batch) {
		s.attempts.Add(1)
		return nil, errors.New("completion unavailable")
	}
	if batch.Mode == "acquire" {
		s.maxBatch.Store(max(s.maxBatch.Load(), int64(len(batch.Keys))))
	}
	return s.recordingStore.UpdateLeases(ctx, batch)
}

func (s *observedLeaseStore) failsBatch(batch idempotency.LeaseBatch) bool {
	if batch.Mode != "complete" || !s.failCompletion.Load() {
		return false
	}
	for _, key := range batch.Keys {
		if key == "retry:money" {
			return true
		}
	}
	return false
}

func TestProcessAcquiresDeclaredHandlerEffectsTogether(t *testing.T) {
	store := &observedLeaseStore{recordingStore: newRecordingStore()}
	d := NewEventDedup(store, "", time.Hour, nil)
	var modules []module.Module
	for _, name := range []string{"first", "second"} {
		m := module.NewModule(name, module.KindCore).ProtectReplay()
		m.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
		modules = append(modules, m.Build())
	}
	p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Dedup: d}, NewRegistry(zap.NewNop(), modules...), Config{})
	t.Cleanup(p.Close)
	msg := commandMsg(t, "batch", "hello")
	require.NoError(t, p.Process(msg))
	awaitCompletion(t, msg, true)
	require.EqualValues(t, 2, store.maxBatch.Load())
}

func TestCompletionRetriesDoNotBlockHealthyEffectsAndSurviveOutage(t *testing.T) {
	store := &observedLeaseStore{recordingStore: newRecordingStore()}
	store.failCompletion.Store(true)
	d := NewEventDedup(store, "", time.Hour, nil)
	t.Cleanup(func() { d.Finish(context.Background()) })
	pending := map[string]*idempotency.Handoff{}
	results := map[string]chan idempotency.Result{}
	for _, identity := range []string{"retry", "healthy"} {
		ctx, work := idempotency.NewWork(t.Context())
		result := make(chan idempotency.Result, 1)
		handoff := &idempotency.Handoff{}
		work.Watch(func(r idempotency.Result) { result <- r })
		require.NoError(t, d.Effect(ctx, EffectRef{Identity: identity, Effect: "money"}, func(ctx context.Context) error { idempotency.Join(ctx, handoff); return nil }))
		work.Seal(nil)
		pending[identity], results[identity] = handoff, result
	}
	pending["retry"].Resolve(nil)
	require.Eventually(t, func() bool { return store.attempts.Load() > 0 }, time.Second, time.Millisecond)
	started := time.Now()
	pending["healthy"].Resolve(nil)
	select {
	case result := <-results["healthy"]:
		require.NoError(t, result.Err)
	case <-time.After(time.Second):
		t.Fatal("healthy completion was blocked")
	}
	require.Less(t, time.Since(started), 100*time.Millisecond)
	require.Eventually(t, func() bool { return store.attempts.Load() > 6 }, 10*time.Second, 10*time.Millisecond)
	require.Empty(t, results["retry"])
	d.Finish(t.Context())
	store.failCompletion.Store(false)
	select {
	case result := <-results["retry"]:
		require.NoError(t, result.Err)
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not recover")
	}
}

func TestProcessChatMetricsCountUpstreamCopiesAcrossPods(t *testing.T) {
	client := valkeytest.Spawn(t)
	for range 2 {
		rec := analytics.New(client, zap.NewNop(), nil)
		t.Cleanup(rec.Close)
		d := recoveryDedup(t, client)
		p := NewPipeline(Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Analytics: rec, Dedup: d}, NewRegistry(zap.NewNop()), Config{})
		t.Cleanup(p.Close)
		msg := commandMsg(t, "synchronous", "hello")
		require.NoError(t, p.Process(msg))
		awaitCompletion(t, msg, true)
	}
	count, err := client.Do(t.Context(), client.B().Hget().Key(catalog.CounterKey(time.Now())).Field("chat.messages").Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
