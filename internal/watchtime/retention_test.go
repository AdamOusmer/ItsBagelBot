// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func retentionAward(t *testing.T, client valkey.Client) data.WatchAwardDTO {
	t.Helper()
	a := testAward()
	a.WindowStartedAtUnixMilli = time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	ctx := t.Context()
	keys := []string{"settings:7", AdmissionKey(7), "live:7"}
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(keys[0]).FieldValue().FieldValue("active", "1").FieldValue("banned", "0").FieldValue("module:loyalty:enabled", "1").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(keys[1]).FieldValue().FieldValue("epoch", a.Generation).FieldValue("instance", strconv.FormatInt(a.AccountCreatedAt, 10)).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Set().Key(keys[2]).Value(a.LiveSession).Build()).Error())
	t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(keys...).Build()) })
	return a
}

func TestWatchRetentionPinsUnpaidAwardsUntilCommit(t *testing.T) {
	client := consumerClient(t)
	ctx := t.Context()
	store := NewStore(client)
	a := retentionAward(t, client)
	accepted, err := store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, accepted)
	desired := time.Now().Add(-HistoryRetention).UnixMilli()
	cutoff, err := store.SafePruneBefore(ctx, desired)
	require.NoError(t, err)
	require.Equal(t, a.WindowStartedAtUnixMilli-1, cutoff)
	// Even a very old accepted award remains payable and replayable until SQL
	// commits; its exact window pins the barrier independently of delivery age.
	accepted, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, accepted)
	consumer := NewConsumer(client, func(_ context.Context, got data.WatchAwardDTO) error {
		require.Equal(t, a, got)
		return nil
	}, zap.NewNop())
	require.NoError(t, consumer.Ensure(ctx))
	require.NoError(t, consumer.read(ctx))
	n, err := client.Do(ctx, client.B().Zcard().Key(OutboxWindowIndex).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, n)
	cutoff, err = store.SafePruneBefore(ctx, desired)
	require.NoError(t, err)
	require.Equal(t, desired, cutoff)
	accepted, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, accepted, "retired replay must not re-enter after its SQL markers are pruned")
}

func TestWatchRetentionLegacyDeliveryBlocksMaintenance(t *testing.T) {
	client := consumerClient(t)
	ctx := t.Context()
	appendAward(t, client, testAward(), OperationID(testAward()))
	called := false
	c := NewConsumer(client, func(context.Context, data.WatchAwardDTO) error { return nil }, zap.NewNop(), WithHistoryMaintenance(func(context.Context, int64) error { called = true; return nil }))
	require.NoError(t, c.pruneHistory(ctx))
	require.False(t, called, "unindexed legacy work must keep all SQL replay protection")
	require.NoError(t, c.Ensure(ctx))
	require.NoError(t, c.read(ctx))
	require.NoError(t, c.pruneHistory(ctx))
	require.True(t, called)
}

func TestWatchRetentionBarrierAndEnqueueAreAtomic(t *testing.T) {
	client := consumerClient(t)
	ctx := t.Context()
	store := NewStore(client)
	a := retentionAward(t, client)
	desired := time.Now().Add(-HistoryRetention).UnixMilli()
	var wg sync.WaitGroup
	var accepted bool
	var enqueueErr, pruneErr error
	wg.Add(2)
	go func() { defer wg.Done(); accepted, enqueueErr = store.Enqueue(ctx, a) }()
	go func() { defer wg.Done(); _, pruneErr = store.SafePruneBefore(ctx, desired) }()
	wg.Wait()
	require.NoError(t, enqueueErr)
	require.NoError(t, pruneErr)
	floor, err := client.Do(ctx, client.B().Get().Key(RetentionFloor).Build()).AsInt64()
	require.NoError(t, err)
	if accepted {
		require.Less(t, floor, a.WindowStartedAtUnixMilli)
	} else {
		require.GreaterOrEqual(t, floor, a.WindowStartedAtUnixMilli)
	}
}

func TestWatchRetentionQuarantineRemovesWindowPin(t *testing.T) {
	client := consumerClient(t)
	ctx := t.Context()
	a := retentionAward(t, client)
	body, err := codec.Marshal(a)
	require.NoError(t, err)
	id, err := client.Do(ctx, client.B().Xadd().Key(Stream).Id("*").FieldValue().FieldValue("operation_id", "mismatched-operation").FieldValue("payload", string(body)).Build()).ToString()
	require.NoError(t, err)
	require.NoError(t, client.Do(ctx, client.B().Zadd().Key(OutboxWindowIndex).ScoreMember().ScoreMember(float64(a.WindowStartedAtUnixMilli), id).Build()).Error())
	c := NewConsumer(client, func(context.Context, data.WatchAwardDTO) error { t.Fatal("invalid award reached SQL"); return nil }, zap.NewNop())
	require.NoError(t, c.Ensure(ctx))
	require.NoError(t, c.read(ctx))
	n, err := client.Do(ctx, client.B().Zcard().Key(OutboxWindowIndex).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestWindowStartUnixMilli(t *testing.T) {
	require.EqualValues(t, 123, WindowStartUnixMilli(data.WatchAwardDTO{WindowID: "7:2:session:123"}))
	require.EqualValues(t, 456, WindowStartUnixMilli(data.WatchAwardDTO{WindowID: "7:2:session:123", WindowStartedAtUnixMilli: 456}))
	require.Zero(t, WindowStartUnixMilli(data.WatchAwardDTO{WindowID: "unknown"}))
}
