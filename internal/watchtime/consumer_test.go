// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func consumerClient(t *testing.T) valkey.Client {
	t.Helper()
	addr := os.Getenv("VALKEY_TEST_ADDR")
	if addr == "" {
		t.Skip("VALKEY_TEST_ADDR required for Stream recovery tests")
	}
	client, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, SelectDB: 15, DisableCache: true})
	require.NoError(t, err)
	require.NoError(t, client.Do(context.Background(), client.B().Del().Key(Stream, QuarantineStream, OutboxWindowIndex, RetentionFloor, "watchtime:operations:7").Build()).Error())
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(Stream, QuarantineStream, OutboxWindowIndex, RetentionFloor, "watchtime:operations:7").Build())
		client.Close()
	})
	return client
}

func testAward() data.WatchAwardDTO {
	return data.WatchAwardDTO{UserID: 7, AccountCreatedAt: 1_700_000_000_000_000, Generation: "2", LiveSession: "session-1", WindowID: "window-1", Entries: []data.LoyaltyEarnEntry{{ViewerID: 8, Points: 10, WatchSeconds: 300}}}
}

func appendAward(t *testing.T, client valkey.Client, award data.WatchAwardDTO, op string) {
	t.Helper()
	body, err := codec.Marshal(award)
	require.NoError(t, err)
	require.NoError(t, client.Do(context.Background(), client.B().Xadd().Key(Stream).Id("*").FieldValue().FieldValue("operation_id", op).FieldValue("payload", string(body)).Build()).Error())
}

func TestWatchConsumerRetainsFailedCommitAndReclaims(t *testing.T) {
	client := consumerClient(t)
	ctx := context.Background()
	a := testAward()
	firstCalls := 0
	first := NewConsumer(client, func(context.Context, data.WatchAwardDTO) error { firstCalls++; return errors.New("SQL unavailable") }, zap.NewNop())
	require.NoError(t, first.Ensure(ctx))
	appendAward(t, client, a, OperationID(a))
	require.NoError(t, client.Do(ctx, client.B().Hset().Key("watchtime:operations:7").FieldValue().FieldValue(OperationID(a), "digest").Build()).Error())
	require.NoError(t, first.read(ctx))
	require.Equal(t, 1, firstCalls)
	n, err := client.Do(ctx, client.B().Xlen().Key(Stream).Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	pending, err := client.Do(ctx, client.B().Xpending().Key(Stream).Group(ConsumerGroup).Build()).ToArray()
	require.NoError(t, err)
	count, err := pending[0].AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	// A replacement process claims the abandoned record. Nothing is ACKed
	// until the repository reports a committed result.
	secondCalls := 0
	second := NewConsumer(client, func(_ context.Context, got data.WatchAwardDTO) error {
		secondCalls++
		require.Equal(t, a, got)
		return nil
	}, zap.NewNop())
	require.NoError(t, second.reclaim(ctx, 0))
	require.Equal(t, 1, secondCalls)
	n, err = client.Do(ctx, client.B().Xlen().Key(Stream).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, n)
	exists, err := client.Do(ctx, client.B().Hexists().Key("watchtime:operations:7").Field(OperationID(a)).Build()).AsBool()
	require.NoError(t, err)
	require.False(t, exists)
	pending, err = client.Do(ctx, client.B().Xpending().Key(Stream).Group(ConsumerGroup).Build()).ToArray()
	require.NoError(t, err)
	count, err = pending[0].AsInt64()
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestWatchConsumerQuarantinesMismatchedIdentityAndContinues(t *testing.T) {
	client := consumerClient(t)
	ctx := context.Background()
	calls := 0
	consumer := NewConsumer(client, func(context.Context, data.WatchAwardDTO) error { calls++; return nil }, zap.NewNop())
	require.NoError(t, consumer.Ensure(ctx))
	a := testAward()
	otherTenant := a
	otherTenant.UserID = 9
	appendAward(t, client, a, OperationID(otherTenant))
	appendAward(t, client, a, OperationID(a))
	require.NoError(t, consumer.read(ctx))
	require.Zero(t, calls)
	n, err := client.Do(ctx, client.B().Xlen().Key(QuarantineStream).Build()).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.NoError(t, consumer.read(ctx))
	require.Equal(t, 1, calls)
	n, err = client.Do(ctx, client.B().Xlen().Key(Stream).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestWatchConsumerRecoveryDrainsBoundedBatch(t *testing.T) {
	client := consumerClient(t)
	ctx := context.Background()
	failed := NewConsumer(client, func(context.Context, data.WatchAwardDTO) error { return errors.New("SQL unavailable") }, zap.NewNop())
	require.NoError(t, failed.Ensure(ctx))
	for i := 0; i < 20; i++ {
		a := testAward()
		a.Chunk = uint32(i)
		appendAward(t, client, a, OperationID(a))
		require.NoError(t, failed.read(ctx))
	}
	committed := 0
	replacement := NewConsumer(client, func(context.Context, data.WatchAwardDTO) error { committed++; return nil }, zap.NewNop())
	require.NoError(t, replacement.recoverBatch(ctx, 0))
	require.Equal(t, 16, committed)
	require.NoError(t, replacement.recoverBatch(ctx, 0))
	require.Equal(t, 20, committed)
	remaining, err := client.Do(ctx, client.B().Xlen().Key(Stream).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, remaining)
}

func TestWatchConsumerHealthReportsStalledAward(t *testing.T) {
	client := consumerClient(t)
	ctx := context.Background()
	consumer := NewConsumer(client, nil, zap.NewNop())
	require.NoError(t, consumer.Check(ctx))
	require.NoError(t, client.Do(ctx, client.B().Xadd().Key(Stream).Id(strconv.FormatInt(time.Now().Add(-6*time.Minute).UnixMilli(), 10)+"-0").FieldValue().FieldValue("payload", "{}").Build()).Error())
	require.ErrorContains(t, consumer.Check(ctx), "pending")
}

func TestValidateWatchAward(t *testing.T) {
	require.NoError(t, ValidateAward(testAward()))
	for _, mutate := range []func(*data.WatchAwardDTO){
		func(a *data.WatchAwardDTO) { a.UserID = 0 },
		func(a *data.WatchAwardDTO) { a.AccountCreatedAt = 0 },
		func(a *data.WatchAwardDTO) { a.Generation = "" },
		func(a *data.WatchAwardDTO) { a.Entries[0].Points = -1 },
		func(a *data.WatchAwardDTO) { a.Entries[0].Points = 1_000_000_001 },
		func(a *data.WatchAwardDTO) { a.Entries[0].WatchSeconds = 301 },
	} {
		a := testAward()
		mutate(&a)
		require.Error(t, ValidateAward(a))
	}
}
