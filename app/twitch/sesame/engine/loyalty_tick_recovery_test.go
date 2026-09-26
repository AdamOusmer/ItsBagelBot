// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWatchTickCrashAfterOutboxReplaysSavedPage(t *testing.T) {
	f := watchFixture(t, 77103)
	ctx := context.Background()
	owner := "crashed-worker"
	result, err := f.clock.eval(ctx, loyaltyClaimScript, []string{loyaltyScheduleKey(f.id), loyaltyDueKey, loyaltyClaimKey(f.id)}, strconv.FormatUint(f.id, 10), owner, strconv.FormatInt(f.now.UnixMilli(), 10), "30000")
	require.NoError(t, err)
	won, err := result.AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, won)
	state, err := f.clock.readSchedule(ctx, f.id)
	require.NoError(t, err)
	require.NoError(t, f.clock.confirmOwned(ctx, f.id, owner, f.now.UnixMilli()))
	dto := data.WatchAwardDTO{UserID: f.id, Generation: state.generation, LiveSession: state.liveSession, AccountCreatedAt: 10000, WindowID: state.window, WindowStartedAtUnixMilli: state.startedAt, Entries: []data.LoyaltyEarnEntry{{ViewerID: 8, Points: 7, WatchSeconds: 300}}}
	body, err := codec.Marshal(dto)
	require.NoError(t, err)
	_, err = f.clock.eval(ctx, loyaltySavePageScript, []string{loyaltyScheduleKey(f.id), loyaltyClaimKey(f.id)}, owner, state.window, string(body), "", "1")
	require.NoError(t, err)
	accepted, err := f.clock.awards.EnqueueOwned(ctx, dto, owner)
	require.NoError(t, err)
	require.True(t, accepted)
	// Simulate process death after durable enqueue but before cursor commit.
	require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(loyaltyClaimKey(f.id)).Build()).Error())
	f.due(t)
	f.clock.request = func(context.Context, string, []byte) (*nats.Msg, error) {
		t.Fatal("saved page must not be fetched again")
		return nil, nil
	}
	f.clock.fire(ctx, f.id)
	require.EqualValues(t, 1, f.operations(t))
	require.Empty(t, f.fields(t)["pending"])
	require.Empty(t, f.fields(t)["window"])
}

func TestWatchTickStaleSavedPageReconfirmsBeforeAcceptance(t *testing.T) {
	for _, live := range []bool{true, false} {
		t.Run(strconv.FormatBool(live), func(t *testing.T) {
			f := watchFixture(t, 77108)
			ctx := context.Background()
			owner := "crashed-before-acceptance"
			_, err := f.clock.eval(ctx, loyaltyClaimScript, []string{loyaltyScheduleKey(f.id), loyaltyDueKey, loyaltyClaimKey(f.id)}, strconv.FormatUint(f.id, 10), owner, strconv.FormatInt(f.now.UnixMilli(), 10), "30000")
			require.NoError(t, err)
			state, err := f.clock.readSchedule(ctx, f.id)
			require.NoError(t, err)
			dto := data.WatchAwardDTO{UserID: f.id, Generation: state.generation, LiveSession: state.liveSession, AccountCreatedAt: 10000, WindowID: state.window, WindowStartedAtUnixMilli: state.startedAt, Entries: []data.LoyaltyEarnEntry{{ViewerID: 8, Points: 7, WatchSeconds: 300}}}
			payload, err := codec.Marshal(dto)
			require.NoError(t, err)
			_, err = f.clock.eval(ctx, loyaltySavePageScript, []string{loyaltyScheduleKey(f.id), loyaltyClaimKey(f.id)}, owner, state.window, string(payload), "", "1")
			require.NoError(t, err)
			require.NoError(t, f.clock.confirmOwned(ctx, f.id, owner, f.now.UnixMilli()))
			require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(loyaltyClaimKey(f.id)).Build()).Error())
			f.now = f.now.Add(2 * time.Hour)
			f.due(t)
			calls := 0
			f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
				calls++
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				require.True(t, req.CheckLive)
				reply := correlatedWatchReply(req)
				reply.CheckedAtUnixMilli, reply.Live, reply.Complete = f.now.UnixMilli(), live, true
				reply.Chatters = []manage.Chatter{{ID: "9"}}
				reply.Error, reply.ErrorCode = "old cursor expired after successful live check", "invalid"
				encoded, err := codec.Marshal(reply)
				return &nats.Msg{Data: encoded}, err
			}
			f.clock.fire(ctx, f.id)
			require.Equal(t, 1, calls)
			if live {
				require.EqualValues(t, 1, f.operations(t))
				require.Empty(t, f.fields(t)["pending"])
				// The operation digest must represent the original saved viewers.
				accepted, err := f.clock.awards.Enqueue(ctx, dto)
				require.NoError(t, err)
				require.True(t, accepted)
			} else {
				require.Zero(t, f.operations(t))
				require.Equal(t, "0", f.fields(t)["active"])
			}
		})
	}
}

func TestWatchWindowCollectionAgeBeginsAtFirstClaim(t *testing.T) {
	f := watchFixture(t, 77201)
	ctx := t.Context()
	// Due time can be ancient after worker downtime; a fresh collection begins
	// when first sampled, rather than pretending the old due time was sampled.
	ancient := f.now.Add(-24 * time.Hour).UnixMilli()
	require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key(loyaltyScheduleKey(f.id)).FieldValue().FieldValue("due", strconv.FormatInt(ancient, 10)).Build()).Error())
	_, err := f.clock.eval(ctx, loyaltyClaimScript, []string{loyaltyScheduleKey(f.id), loyaltyDueKey, loyaltyClaimKey(f.id)}, strconv.FormatUint(f.id, 10), "first-claim", strconv.FormatInt(f.now.UnixMilli(), 10), "30000")
	require.NoError(t, err)
	fields := f.fields(t)
	require.Equal(t, strconv.FormatInt(f.now.UnixMilli(), 10), fields["window_started_at"])
	require.Contains(t, fields["window"], strconv.FormatInt(ancient, 10))
}

func TestWatchExpiredUnsampledCursorAbandonedBeforeHTTP(t *testing.T) {
	f := watchFixture(t, 77202)
	ctx := t.Context()
	require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key(loyaltyScheduleKey(f.id)).FieldValue().FieldValue("window", "old-window").FieldValue("window_started_at", strconv.FormatInt(f.now.Add(-watchCollectionMaxAge).UnixMilli(), 10)).FieldValue("cursor", "unsampled-old-page").Build()).Error())
	f.clock.request = func(context.Context, string, []byte) (*nats.Msg, error) {
		t.Fatal("expired unsampled cursor must be abandoned before HTTP")
		return nil, nil
	}
	f.clock.fire(ctx, f.id)
	fields := f.fields(t)
	require.Empty(t, fields["window"])
	require.Empty(t, fields["cursor"])
	require.Empty(t, fields["pending"])
	require.Equal(t, "1", fields["active"])
	require.Equal(t, "collection age exceeded", fields["last_error"])
	require.Equal(t, strconv.FormatInt(f.now.Add(watchTickInterval).UnixMilli(), 10), fields["due"])
	require.Zero(t, f.operations(t))
}

func TestWatchExpiredSavedPageReconfirmationPreservesActualStreamBoundary(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(strconv.FormatBool(changed), func(t *testing.T) {
			f := watchFixture(t, 77203)
			ctx := t.Context()
			owner := "saved-page-worker"
			_, err := f.clock.eval(ctx, loyaltyClaimScript, []string{loyaltyScheduleKey(f.id), loyaltyDueKey, loyaltyClaimKey(f.id)}, strconv.FormatUint(f.id, 10), owner, strconv.FormatInt(f.now.UnixMilli(), 10), "30000")
			require.NoError(t, err)
			state, err := f.clock.readSchedule(ctx, f.id)
			require.NoError(t, err)
			require.NoError(t, f.clock.confirmOwned(ctx, f.id, owner, f.now.UnixMilli()))
			award := data.WatchAwardDTO{UserID: f.id, AccountCreatedAt: 10000, Generation: state.generation, LiveSession: state.liveSession, WindowID: state.window, WindowStartedAtUnixMilli: state.startedAt, Entries: []data.LoyaltyEarnEntry{{ViewerID: 8, Points: 7, WatchSeconds: 300}}}
			payload, err := codec.Marshal(award)
			require.NoError(t, err)
			_, err = f.clock.eval(ctx, loyaltySavePageScript, []string{loyaltyScheduleKey(f.id), loyaltyClaimKey(f.id)}, owner, state.window, string(payload), "remaining-expired-cursor", "0")
			require.NoError(t, err)
			require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(loyaltyClaimKey(f.id)).Build()).Error())
			f.now = f.now.Add(2 * time.Hour)
			f.due(t)
			calls := 0
			f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
				calls++
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				require.True(t, req.CheckLive)
				reply := correlatedWatchReply(req)
				reply.CheckedAtUnixMilli = f.now.UnixMilli()
				reply.Live = true
				reply.Chatters = []manage.Chatter{{ID: "9"}}
				if changed {
					reply.StreamID = "stream-2"
					reply.StreamStartedAtUnixMilli = f.now.Add(-time.Minute).UnixMilli()
				}
				reply.Error = "old cursor expired"
				reply.ErrorCode = "invalid"
				b, err := codec.Marshal(reply)
				return &nats.Msg{Data: b}, err
			}
			f.clock.fire(ctx, f.id)
			require.Equal(t, 1, calls)
			fields := f.fields(t)
			require.Equal(t, "2000", fields["session"], "provider identity changes independently of local event version")
			require.Empty(t, fields["window"])
			require.Empty(t, fields["pending"])
			require.Empty(t, fields["cursor"])
			require.Equal(t, "1", fields["active"])
			require.Equal(t, strconv.FormatInt(f.now.Add(watchTickInterval).UnixMilli(), 10), fields["due"])
			if changed {
				require.Zero(t, f.operations(t))
				require.Equal(t, "stream-2", fields["provider_stream_id"])
				require.Equal(t, "provider stream changed", fields["last_error"])
			} else {
				require.EqualValues(t, 1, f.operations(t))
				require.Equal(t, "stream-1", fields["provider_stream_id"])
				require.Equal(t, "collection age exceeded after saved page", fields["last_error"])
				digest, err := f.client.Do(ctx, f.client.B().Hget().Key("watchtime:operations:"+strconv.FormatUint(f.id, 10)).Field(watchtime.OperationID(award)).Build()).ToString()
				require.NoError(t, err)
				require.NotEmpty(t, digest, "only immutable saved viewers reached the outbox")
			}
		})
	}
}

func TestWatchTickDiscoveryResumesPersistedPendingPage(t *testing.T) {
	f := watchFixture(t, 77111)
	ctx := context.Background()
	globalKeys := []string{loyaltyDiscoveryCursorKey, loyaltyDiscoveryQueueKey, loyaltyReconcileClaimKey}
	require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(globalKeys...).Build()).Error())
	t.Cleanup(func() { f.client.Do(ctx, f.client.B().Del().Key(globalKeys...).Build()) })
	require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(loyaltyScheduleKey(f.id)).Build()).Error())
	require.NoError(t, f.client.Do(ctx, f.client.B().Zrem().Key(loyaltyDueKey).Member(strconv.FormatUint(f.id, 10)).Build()).Error())
	_, err := f.clock.eval(ctx, loyaltyDiscoverySaveScript, []string{loyaltyDiscoveryCursorKey, loyaltyDiscoveryQueueKey}, "0", strconv.FormatUint(f.id, 10))
	require.NoError(t, err)
	// A replica resumes the saved page before starting another keyspace scan.
	replica := NewValkeyLoyaltyClock(f.client, nil, nil, nil, nil, LoyaltyClockConfig{Log: zap.NewNop()})
	replica.reconcile(ctx)
	require.Equal(t, "1", f.fields(t)["active"])
	remaining, err := f.client.Do(ctx, f.client.B().Llen().Key(loyaltyDiscoveryQueueKey).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, remaining)
}
