// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package watchtime_test

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	settle = 15 * time.Second
	poll   = 10 * time.Millisecond
)

func run(t *testing.T, c *watchtime.Consumer) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	stop = func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return stop
}

func failing(calls *atomic.Int64) func(context.Context, data.WatchAwardDTO) error {
	return func(context.Context, data.WatchAwardDTO) error {
		calls.Add(1)
		return errors.New("SQL unavailable")
	}
}

func committing(calls *atomic.Int64) func(context.Context, data.WatchAwardDTO) error {
	return func(context.Context, data.WatchAwardDTO) error {
		calls.Add(1)
		return nil
	}
}

func count(client valkey.Client, cmd valkey.Completed) int64 {
	n, err := client.Do(context.Background(), cmd).AsInt64()
	if err != nil {
		return -1
	}
	return n
}

func outboxLen(c valkey.Client) int64 { return count(c, c.B().Xlen().Key(watchtime.Stream).Build()) }

func quarantineLen(c valkey.Client) int64 {
	return count(c, c.B().Xlen().Key(watchtime.QuarantineStream).Build())
}

func windowPins(c valkey.Client) int64 {
	return count(c, c.B().Zcard().Key(watchtime.OutboxWindowIndex).Build())
}

func pending(t *testing.T, client valkey.Client) int64 {
	t.Helper()
	summary, err := client.Do(t.Context(), client.B().Xpending().Key(watchtime.Stream).Group(watchtime.ConsumerGroup).Build()).ToArray()
	require.NoError(t, err)
	count, err := summary[0].AsInt64()
	require.NoError(t, err)
	return count
}

func abandon(t *testing.T, client valkey.Client, ids ...string) {
	t.Helper()
	cmd := client.B().Xclaim().Key(watchtime.Stream).Group(watchtime.ConsumerGroup).Consumer("crashed-replica").MinIdleTime("0").Id(ids...).Idle(120_000).Build()
	require.NoError(t, client.Do(t.Context(), cmd).Error())
}

func TestConsumerRetainsFailedCommitAndReclaimsItAfterTheClaimIdle(t *testing.T) {
	client := outboxClient(t)
	a := testAward()
	var firstCalls, secondCalls atomic.Int64
	id := appendAward(t, client, a, watchtime.OperationID(a))
	require.NoError(t, client.Do(t.Context(), client.B().Hset().Key("watchtime:operations:7").FieldValue().FieldValue(watchtime.OperationID(a), "digest").Build()).Error())
	first := watchtime.NewConsumer(client, failing(&firstCalls), zap.NewNop())
	require.NoError(t, first.Ensure(t.Context()))

	stop := run(t, first)
	require.Eventually(t, func() bool { return firstCalls.Load() == 1 }, settle, poll)
	stop()

	assert.EqualValues(t, 1, outboxLen(client), "nothing is removed until the repository commits")
	assert.EqualValues(t, 1, pending(t, client))
	abandon(t, client, id)
	second := watchtime.NewConsumer(client, func(_ context.Context, got data.WatchAwardDTO) error {
		secondCalls.Add(1)
		assert.Equal(t, a, got)
		return nil
	}, zap.NewNop())
	run(t, second)
	require.Eventually(t, func() bool { return outboxLen(client) == 0 }, settle, poll)
	assert.EqualValues(t, 1, secondCalls.Load())
	exists, err := client.Do(t.Context(), client.B().Hexists().Key("watchtime:operations:7").Field(watchtime.OperationID(a)).Build()).AsBool()
	require.NoError(t, err)
	assert.False(t, exists, "the acceptance digest is cleared with the delivery")
	assert.Zero(t, pending(t, client))
}

func TestConsumerQuarantinesMismatchedIdentityAndContinues(t *testing.T) {
	client := outboxClient(t)
	var calls atomic.Int64
	a := testAward()
	otherTenant := a
	otherTenant.UserID = 9
	appendAward(t, client, a, watchtime.OperationID(otherTenant))
	appendAward(t, client, a, watchtime.OperationID(a))

	run(t, watchtime.NewConsumer(client, committing(&calls), zap.NewNop()))

	require.Eventually(t, func() bool { return calls.Load() == 1 && outboxLen(client) == 0 }, settle, poll)
	assert.EqualValues(t, 1, quarantineLen(client), "the mismatched delivery is kept for inspection")
	assert.EqualValues(t, 1, calls.Load(), "only the valid delivery reaches the repository")
}

func TestConsumerRecoversStrandedDeliveriesInBoundedBatches(t *testing.T) {
	client := outboxClient(t)
	var failedCalls, committed atomic.Int64
	failed := watchtime.NewConsumer(client, failing(&failedCalls), zap.NewNop())
	require.NoError(t, failed.Ensure(t.Context()))
	ids := make([]string, 20)
	for i := range ids {
		a := testAward()
		a.Chunk = uint32(i)
		ids[i] = appendAward(t, client, a, watchtime.OperationID(a))
	}
	stop := run(t, failed)
	require.Eventually(t, func() bool { return failedCalls.Load() == 20 }, settle, poll)
	stop()
	abandon(t, client, ids...)

	run(t, watchtime.NewConsumer(client, committing(&committed), zap.NewNop()))

	require.Eventually(t, func() bool { return committed.Load() == 16 }, settle, poll, "one recovery pass commits at most 16")
	require.Eventually(t, func() bool { return committed.Load() == 20 }, settle, poll)
	assert.Zero(t, outboxLen(client))
}

func TestConsumerCheckReportsStalledAward(t *testing.T) {
	client := outboxClient(t)
	consumer := watchtime.NewConsumer(client, nil, zap.NewNop())
	require.NoError(t, consumer.Check(t.Context()))
	stale := strconv.FormatInt(time.Now().Add(-6*time.Minute).UnixMilli(), 10) + "-0"
	require.NoError(t, client.Do(t.Context(), client.B().Xadd().Key(watchtime.Stream).Id(stale).FieldValue().FieldValue("payload", "{}").Build()).Error())

	assert.ErrorContains(t, consumer.Check(t.Context()), "pending")
}

func retentionAward(t *testing.T, client valkey.Client) data.WatchAwardDTO {
	t.Helper()
	a := testAward()
	a.WindowStartedAtUnixMilli = time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	keys := []string{"settings:7", watchtime.AdmissionKey(7), "live:7"}
	ctx := t.Context()
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(keys[0]).FieldValue().FieldValue("active", "1").FieldValue("banned", "0").FieldValue("module:loyalty:enabled", "1").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(keys[1]).FieldValue().FieldValue("epoch", a.Generation).FieldValue("instance", strconv.FormatInt(a.AccountCreatedAt, 10)).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Set().Key(keys[2]).Value(a.LiveSession).Build()).Error())
	cleanupKeys(t, client, keys...)
	return a
}

func TestRetentionPinsUnpaidAwardsUntilCommit(t *testing.T) {
	client := outboxClient(t)
	ctx := t.Context()
	store := watchtime.NewStore(client)
	a := retentionAward(t, client)
	accepted, err := store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, accepted)
	desired := time.Now().Add(-watchtime.HistoryRetention).UnixMilli()
	cutoff, err := store.SafePruneBefore(ctx, desired)
	require.NoError(t, err)
	require.Equal(t, a.WindowStartedAtUnixMilli-1, cutoff)
	// Even a very old accepted award remains payable and replayable until SQL
	// commits; its exact window pins the barrier independently of delivery age.
	accepted, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.True(t, accepted)

	run(t, watchtime.NewConsumer(client, func(_ context.Context, got data.WatchAwardDTO) error {
		assert.Equal(t, a, got)
		return nil
	}, zap.NewNop()))

	require.Eventually(t, func() bool { return windowPins(client) == 0 }, settle, poll)
	cutoff, err = store.SafePruneBefore(ctx, desired)
	require.NoError(t, err)
	require.Equal(t, desired, cutoff)
	accepted, err = store.Enqueue(ctx, a)
	require.NoError(t, err)
	require.False(t, accepted, "retired replay must not re-enter after its SQL markers are pruned")
}

func TestRetentionLegacyDeliveryBlocksTheBarrierUntilDrained(t *testing.T) {
	client := outboxClient(t)
	store := watchtime.NewStore(client)
	var calls atomic.Int64
	desired := time.Now().Add(-watchtime.HistoryRetention).UnixMilli()
	appendAward(t, client, testAward(), watchtime.OperationID(testAward()))

	blocked, err := store.SafePruneBefore(t.Context(), desired)
	require.NoError(t, err)
	require.Zero(t, blocked, "unindexed legacy work must keep all SQL replay protection")

	run(t, watchtime.NewConsumer(client, committing(&calls), zap.NewNop()))
	require.Eventually(t, func() bool { return calls.Load() == 1 && outboxLen(client) == 0 }, settle, poll)

	cutoff, err := store.SafePruneBefore(t.Context(), desired)
	require.NoError(t, err)
	require.Equal(t, desired, cutoff)
}

func TestConsumerHandsTheSafeCutoffToHistoryMaintenance(t *testing.T) {
	client := outboxClient(t)
	cutoffs := make(chan int64, 4)
	maintain := watchtime.WithHistoryMaintenance(func(_ context.Context, cutoff int64) error {
		cutoffs <- cutoff
		return nil
	})

	run(t, watchtime.NewConsumer(client, committing(new(atomic.Int64)), zap.NewNop(), maintain))

	select {
	case cutoff := <-cutoffs:
		assert.Positive(t, cutoff)
	case <-time.After(settle):
		t.Fatal("history maintenance never ran")
	}
}

func TestRetentionBarrierAndEnqueueAreAtomic(t *testing.T) {
	client := outboxClient(t)
	ctx := t.Context()
	store := watchtime.NewStore(client)
	a := retentionAward(t, client)
	desired := time.Now().Add(-watchtime.HistoryRetention).UnixMilli()
	var accepted bool
	var enqueueErr, pruneErr error
	done := make(chan struct{}, 2)
	go func() { accepted, enqueueErr = store.Enqueue(ctx, a); done <- struct{}{} }()
	go func() { _, pruneErr = store.SafePruneBefore(ctx, desired); done <- struct{}{} }()
	<-done
	<-done
	require.NoError(t, enqueueErr)
	require.NoError(t, pruneErr)
	floor, err := client.Do(ctx, client.B().Get().Key(watchtime.RetentionFloor).Build()).AsInt64()
	require.NoError(t, err)
	if accepted {
		require.Less(t, floor, a.WindowStartedAtUnixMilli)
	} else {
		require.GreaterOrEqual(t, floor, a.WindowStartedAtUnixMilli)
	}
}

func TestRetentionQuarantineRemovesWindowPin(t *testing.T) {
	client := outboxClient(t)
	a := retentionAward(t, client)
	id := appendAward(t, client, a, "mismatched-operation")
	require.NoError(t, client.Do(t.Context(), client.B().Zadd().Key(watchtime.OutboxWindowIndex).ScoreMember().ScoreMember(float64(a.WindowStartedAtUnixMilli), id).Build()).Error())
	reached := func(context.Context, data.WatchAwardDTO) error {
		t.Error("invalid award reached SQL")
		return nil
	}

	run(t, watchtime.NewConsumer(client, reached, zap.NewNop()))

	require.Eventually(t, func() bool { return windowPins(client) == 0 }, settle, poll)
	assert.EqualValues(t, 1, quarantineLen(client))
}

func TestValidateAwardRefusesIncompleteOrOutOfRangeAwards(t *testing.T) {
	require.NoError(t, watchtime.ValidateAward(testAward()))
	for name, mutate := range map[string]func(*data.WatchAwardDTO){
		"zero user":                func(a *data.WatchAwardDTO) { a.UserID = 0 },
		"zero account creation":    func(a *data.WatchAwardDTO) { a.AccountCreatedAt = 0 },
		"empty generation":         func(a *data.WatchAwardDTO) { a.Generation = "" },
		"negative points":          func(a *data.WatchAwardDTO) { a.Entries[0].Points = -1 },
		"points above the ceiling": func(a *data.WatchAwardDTO) { a.Entries[0].Points = 1_000_000_001 },
		"watch seconds above 300":  func(a *data.WatchAwardDTO) { a.Entries[0].WatchSeconds = 301 },
	} {
		t.Run(name, func(t *testing.T) {
			a := testAward()
			mutate(&a)
			assert.Error(t, watchtime.ValidateAward(a))
		})
	}
}

func TestWindowStartUnixMilli(t *testing.T) {
	cases := []struct {
		name string
		in   data.WatchAwardDTO
		want int64
	}{
		{"legacy window id carries the start", data.WatchAwardDTO{WindowID: "7:2:session:123"}, 123},
		{"explicit start wins over the window id", data.WatchAwardDTO{WindowID: "7:2:session:123", WindowStartedAtUnixMilli: 456}, 456},
		{"unknown age stays zero", data.WatchAwardDTO{WindowID: "unknown"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, watchtime.WindowStartUnixMilli(tc.in))
		})
	}
}
