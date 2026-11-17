// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/idempotency"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func recoveryDedup(t *testing.T, client valkey.Client) *EventDedup {
	t.Helper()
	d := NewEventDedup(idempotency.NewValkeyStore(client, "recovery:", nil), "recovery:", 0, nil)
	t.Cleanup(func() { d.Finish(context.Background()) })
	return d
}

func TestEffectCompletionHandlesDeletedAndForeignLeases(t *testing.T) {
	for _, replacement := range []string{"", "foreign"} {
		t.Run("replacement="+replacement, func(t *testing.T) {
			client := valkeytest.Spawn(t)
			d := recoveryDedup(t, client)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			ref := EffectRef{Identity: "completion", Effect: "money"}
			key := "recovery:completion:money"
			err := d.Effect(ctx, ref, func(context.Context) error {
				if replacement == "" {
					return client.Do(ctx, client.B().Del().Key(key).Build()).Error()
				}
				return client.Do(ctx, client.B().Set().Key(key).Value(replacement).Build()).Error()
			})
			require.NoError(t, err)
			value, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
			require.NoError(t, err)
			want := "1"
			if replacement != "" {
				want = replacement
			}
			require.Equal(t, want, value)
			conflicts := int64(0)
			if replacement != "" {
				conflicts = 1
			}
			require.Equal(t, conflicts, d.LeaseConflicts())
		})
	}
}

func TestEffectCrashStopsRenewalWithinStreamHorizon(t *testing.T) {
	client := valkeytest.Spawn(t)
	first, peer := recoveryDedup(t, client), recoveryDedup(t, client)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, work := idempotency.NewWork(ctx)
	pending := &idempotency.Handoff{}
	ref := EffectRef{Identity: "crashed", Effect: "money"}
	require.NoError(t, first.Effect(ctx, ref, func(ctx context.Context) error { idempotency.Join(ctx, pending); return nil }))
	work.Seal(nil)
	cancel()
	var applied atomic.Int64
	retryCtx, retry := idempotency.NewWork(t.Context())
	require.ErrorAs(t, peer.Effect(retryCtx, ref, func(context.Context) error { applied.Add(1); return nil }), new(effectBusy))
	retry.Seal(nil)
	require.Eventually(t, func() bool {
		exists, err := client.Do(t.Context(), client.B().Exists().Key("recovery:crashed:money").Build()).AsInt64()
		return err == nil && exists == 0
	}, 14*time.Second, 25*time.Millisecond)
	require.NoError(t, peer.Effect(t.Context(), ref, func(context.Context) error { applied.Add(1); return nil }))
	require.EqualValues(t, 1, applied.Load())
	pending.Resolve(nil)
}

func TestEffectDetectsLostPendingLeaseAndPreservesForeignCompletion(t *testing.T) {
	client := valkeytest.Spawn(t)
	d := recoveryDedup(t, client)
	ctx, work := idempotency.NewWork(t.Context())
	pending := &idempotency.Handoff{}
	ref := EffectRef{Identity: "lost", Effect: "money"}
	require.NoError(t, d.Effect(ctx, ref, func(ctx context.Context) error { idempotency.Join(ctx, pending); return nil }))
	work.Seal(nil)
	require.NoError(t, client.Do(t.Context(), client.B().Set().Key("recovery:lost:money").Value("peer").Build()).Error())
	require.Eventually(t, func() bool { return d.LeaseConflicts() > 0 }, 5*time.Second, 10*time.Millisecond)
	done := make(chan idempotency.Result, 1)
	work.Watch(func(r idempotency.Result) { done <- r })
	pending.Resolve(nil)
	select {
	case r := <-done:
		require.NoError(t, r.Err)
	case <-time.After(time.Second):
		t.Fatal("lost lease wedged completion")
	}
	value, err := client.Do(t.Context(), client.B().Get().Key("recovery:lost:money").Build()).ToString()
	require.NoError(t, err)
	require.Equal(t, "peer", value)
}

type unavailableLeaseStore struct{ *idempotency.ValkeyStore }

func (s unavailableLeaseStore) UpdateLeases(ctx context.Context, batch idempotency.LeaseBatch) ([]int64, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEffectAcquisitionHonorsWorkerCancellation(t *testing.T) {
	client := valkeytest.Spawn(t)
	d := NewEventDedup(unavailableLeaseStore{idempotency.NewValkeyStore(client, "", nil)}, "", 0, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	called := false
	started := time.Now()
	err := d.Effect(ctx, EffectRef{Identity: "canceled", Effect: "money"}, func(context.Context) error { called = true; return nil })
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	require.False(t, called)
	require.Less(t, time.Since(started), 50*time.Millisecond)
}

func TestEffectFinishPreservesPendingLeaseUntilExpiry(t *testing.T) {
	client := valkeytest.Spawn(t)
	first, peer := recoveryDedup(t, client), recoveryDedup(t, client)
	ctx, work := idempotency.NewWork(t.Context())
	pending := &idempotency.Handoff{}
	ref := EffectRef{Identity: "finished", Effect: "money"}
	require.NoError(t, first.Effect(ctx, ref, func(ctx context.Context) error { idempotency.Join(ctx, pending); return nil }))
	work.Seal(nil)
	first.Finish(t.Context())
	ran := false
	peerCtx, peerWork := idempotency.NewWork(t.Context())
	require.ErrorAs(t, peer.Effect(peerCtx, ref, func(context.Context) error { ran = true; return nil }), new(effectBusy))
	peerWork.Seal(nil)
	require.False(t, ran)
	pending.Resolve(nil)
}

func TestCommittedEffectKeepsReceiptWhenOtherWorkFails(t *testing.T) {
	client := valkeytest.Spawn(t)
	d := recoveryDedup(t, client)
	ctx, work := idempotency.NewWork(t.Context())
	ref := EffectRef{Identity: "committed", Effect: "money"}
	mutations := 0
	require.NoError(t, d.Effect(ctx, ref, func(ctx context.Context) error {
		mutations++
		idempotency.Fail(ctx, errors.New("reply unavailable"))
		return nil
	}))
	work.Seal(nil)
	waitEffect(t.Context(), &work.Handoff)
	require.NoError(t, d.Effect(t.Context(), ref, func(context.Context) error { mutations++; return nil }))
	require.Equal(t, 1, mutations)
}

func TestEffectWithoutHotDeliveryRetainsConfiguredReceiptHorizon(t *testing.T) {
	client := valkeytest.Spawn(t)
	d := NewEventDedup(idempotency.NewValkeyStore(client, "long:", nil), "long:", time.Hour, nil)
	t.Cleanup(func() { d.Finish(t.Context()) })
	require.NoError(t, d.Effect(t.Context(), EffectRef{Identity: "rpc", Effect: "money"}, func(context.Context) error { return nil }))
	ttl, err := client.Do(t.Context(), client.B().Pttl().Key("long:rpc:money").Build()).AsInt64()
	require.NoError(t, err)
	require.InDelta(t, time.Hour.Milliseconds(), ttl, 1000)
}
