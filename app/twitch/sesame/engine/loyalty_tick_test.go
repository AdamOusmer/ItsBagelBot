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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func TestRearmAfterFailure(t *testing.T) {
	assert.Equal(t, watchTickQuickRetry, rearmAfterFailure(1))
	assert.Equal(t, watchTickQuickRetry, rearmAfterFailure(watchTickQuickRetries))
	assert.Equal(t, watchTickInterval, rearmAfterFailure(watchTickQuickRetries+1))
}

type loyaltyClockFixture struct {
	clock  *ValkeyLoyaltyClock
	client valkey.Client
	id     uint64
	now    time.Time
}

func watchFixture(t *testing.T, id uint64) *loyaltyClockFixture {
	t.Helper()
	client := newHotPathTestClient(t)
	ctx := context.Background()
	sid := strconv.FormatUint(id, 10)
	keys := []string{"settings:" + sid, "live:" + sid, watchtime.AdmissionKey(id), loyaltyScheduleKey(id), loyaltyClaimKey(id), "watchtime:operations:" + sid, loyaltyTickKey(id)}
	require.NoError(t, client.Do(ctx, client.B().Del().Key(keys...).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Zrem().Key(loyaltyDueKey).Member(sid).Build()).Error())
	t.Cleanup(func() {
		client.Do(ctx, client.B().Del().Key(keys...).Build())
		client.Do(ctx, client.B().Zrem().Key(loyaltyDueKey).Member(sid).Build())
		client.Do(ctx, client.B().Srem().Key("trial:desired").Member(sid).Build())
		entries, err := client.Do(ctx, client.B().Xrange().Key(watchtime.Stream).Start("-").End("+").Build()).AsXRange()
		if err == nil {
			for _, entry := range entries {
				var award data.WatchAwardDTO
				if codec.Unmarshal([]byte(entry.FieldValues["payload"]), &award) == nil && award.UserID == id {
					client.Do(ctx, client.B().Eval().Script("redis.call('XDEL',KEYS[1],ARGV[1]); redis.call('ZREM',KEYS[2],ARGV[1]); return 1").Numkeys(2).Key(watchtime.Stream, watchtime.OutboxWindowIndex).Arg(entry.ID).Build())
				}
			}
		}

	})
	require.NoError(t, client.Do(ctx, client.B().Hset().Key("settings:"+sid).FieldValue().FieldValue("active", "1").FieldValue("banned", "0").FieldValue("module:loyalty:enabled", "1").FieldValue("module:loyalty:config", `{"watchPointsPerTick":7}`).Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Hset().Key(watchtime.AdmissionKey(id)).FieldValue().FieldValue("epoch", "1").FieldValue("instance", "10000").Build()).Error())
	require.NoError(t, client.Do(ctx, client.B().Set().Key("live:"+sid).Value("2000").Build()).Error())
	f := &loyaltyClockFixture{client: client, id: id, now: time.Now()}
	f.clock = NewValkeyLoyaltyClock(client, nil, nil, nil, nil, LoyaltyClockConfig{BotUserID: "99", Log: zap.NewNop()})
	f.clock.now = func() time.Time { return f.now }
	f.clock.ArmVersioned(ctx, id, 2000)
	f.due(t)
	return f
}

func (f *loyaltyClockFixture) due(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	sid := strconv.FormatUint(f.id, 10)
	require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key(loyaltyScheduleKey(f.id)).FieldValue().FieldValue("due", strconv.FormatInt(f.now.Add(-watchTickInterval).UnixMilli(), 10)).Build()).Error())
	require.NoError(t, f.client.Do(ctx, f.client.B().Zadd().Key(loyaltyDueKey).ScoreMember().ScoreMember(float64(f.now.UnixMilli()-1), sid).Build()).Error())
}

func (f *loyaltyClockFixture) fields(t *testing.T) map[string]string {
	t.Helper()
	fields, err := f.client.Do(context.Background(), f.client.B().Hgetall().Key(loyaltyScheduleKey(f.id)).Build()).AsStrMap()
	require.NoError(t, err)
	return fields
}

func (f *loyaltyClockFixture) operations(t *testing.T) int64 {
	t.Helper()
	n, err := f.client.Do(context.Background(), f.client.B().Hlen().Key("watchtime:operations:"+strconv.FormatUint(f.id, 10)).Build()).AsInt64()
	require.NoError(t, err)
	return n
}

func correlatedWatchReply(req manage.ChattersRequest) manage.ChattersReply {
	return manage.ChattersReply{BroadcasterID: req.BroadcasterID, RequestID: req.RequestID, WindowID: req.WindowID, SessionGeneration: req.SessionGeneration, LiveSession: req.LiveSession, StreamID: "stream-1", StreamStartedAtUnixMilli: 1_700_000_000_000}
}

func (s *ValkeyLoyaltyClock) confirmOwned(ctx context.Context, id uint64, owner string, at int64) error {
	page := loyaltyPage{clock: s, id: id, owner: owner}
	_, err := page.confirmStream(ctx, manage.ChattersReply{CheckedAtUnixMilli: at, StreamID: "stream-1", StreamStartedAtUnixMilli: 1_700_000_000_000})
	return err
}

func TestWatchTickVersionedOfflinePreservesNewerSession(t *testing.T) {
	f := watchFixture(t, 77101)
	ctx := context.Background()
	before := f.fields(t)
	f.clock.DisarmVersioned(ctx, f.id, 1000)
	fields := f.fields(t)
	require.Equal(t, "1", fields["active"])
	require.Equal(t, before["due"], fields["due"])
	f.clock.DisarmVersioned(ctx, f.id, 3000)
	require.Equal(t, "0", f.fields(t)["active"])
	_, err := f.client.Do(ctx, f.client.B().Zscore().Key(loyaltyDueKey).Member(strconv.FormatUint(f.id, 10)).Build()).ToString()
	require.True(t, valkey.IsValkeyNil(err))
	f.clock.ArmVersioned(ctx, f.id, 2000)
	require.Equal(t, "0", f.fields(t)["active"], "stale online cannot undo newer offline")
}

func TestWatchTickRetryStateSharedAndTypedDelay(t *testing.T) {
	f := watchFixture(t, 77104)
	ctx := context.Background()
	retryAt := f.now.Add(10 * time.Minute).UnixMilli()
	for i := 1; i <= 3; i++ {
		c := NewValkeyLoyaltyClock(f.client, nil, nil, nil, nil, LoyaltyClockConfig{})
		c.now = func() time.Time { return f.now }
		require.NoError(t, f.client.Do(ctx, f.client.B().Zadd().Key(loyaltyDueKey).ScoreMember().ScoreMember(float64(f.now.UnixMilli()-1), strconv.FormatUint(f.id, 10)).Build()).Error())
		c.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
			var req manage.ChattersRequest
			require.NoError(t, codec.Unmarshal(body, &req))
			reply := correlatedWatchReply(req)
			reply.Error = "quota exhausted"
			reply.ErrorCode = "rate_limited"
			reply.RetryAtUnixMilli = retryAt
			encoded, err := codec.Marshal(reply)
			return &nats.Msg{Data: encoded}, err
		}
		c.fire(ctx, f.id)
		fields := f.fields(t)
		require.Equal(t, strconv.Itoa(i), fields["failures"])
		require.Equal(t, strconv.FormatInt(retryAt, 10), fields["retry_at"])
	}
}

func TestWatchTickTenantAndLeaseFences(t *testing.T) {
	for _, scenario := range []string{"trial", "inactive", "lease lost", "new session"} {
		t.Run(scenario, func(t *testing.T) {
			f := watchFixture(t, 77105)
			ctx := context.Background()
			sid := strconv.FormatUint(f.id, 10)
			f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				reply := correlatedWatchReply(req)
				reply.CheckedAtUnixMilli = f.now.UnixMilli()
				reply.Live = true
				reply.Complete = true
				reply.Chatters = []manage.Chatter{{ID: "8"}}
				switch scenario {
				case "trial":
					require.NoError(t, f.client.Do(ctx, f.client.B().Sadd().Key("trial:desired").Member(sid).Build()).Error())
				case "inactive":
					require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key("settings:"+sid).FieldValue().FieldValue("active", "0").Build()).Error())
				case "lease lost":
					require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key(loyaltyClaimKey(f.id)).Value("different-worker").PxMilliseconds(30000).Build()).Error())
				case "new session":
					f.clock.ArmVersioned(ctx, f.id, 3000)
				}
				encoded, err := codec.Marshal(reply)
				return &nats.Msg{Data: encoded}, err
			}
			f.clock.fire(ctx, f.id)
			require.Zero(t, f.operations(t), "revoked/expired workers cannot enqueue awards")
			if scenario == "new session" {
				require.Equal(t, "1", f.fields(t)["active"])
				require.Equal(t, "3000", f.fields(t)["version"], "late worker must not stop new session")
			}
		})
	}
}

func TestWatchTickEmptyPageAndOfflineConfirmation(t *testing.T) {
	for _, live := range []bool{true, false} {
		t.Run(strconv.FormatBool(live), func(t *testing.T) {
			f := watchFixture(t, 77107)
			f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				reply := correlatedWatchReply(req)
				reply.CheckedAtUnixMilli = f.now.UnixMilli()
				reply.Live = live
				reply.Complete = true
				encoded, err := codec.Marshal(reply)
				return &nats.Msg{Data: encoded}, err
			}
			f.clock.fire(context.Background(), f.id)
			require.Zero(t, f.operations(t))
			if live {
				require.Empty(t, f.fields(t)["window"])
			} else {
				require.Equal(t, "0", f.fields(t)["active"])
			}
		})
	}
}

func TestWatchTickCompletionPreservesCadenceAfterSlowPage(t *testing.T) {
	f := watchFixture(t, 77110)
	ctx := context.Background()
	original, err := strconv.ParseInt(f.fields(t)["due"], 10, 64)
	require.NoError(t, err)
	f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
		var req manage.ChattersRequest
		require.NoError(t, codec.Unmarshal(body, &req))
		f.now = f.now.Add(30 * time.Second)
		reply := correlatedWatchReply(req)
		reply.CheckedAtUnixMilli, reply.Live, reply.Complete = f.now.UnixMilli(), true, true
		encoded, err := codec.Marshal(reply)
		return &nats.Msg{Data: encoded}, err
	}
	f.clock.fire(ctx, f.id)
	next, err := strconv.ParseInt(f.fields(t)["due"], 10, 64)
	require.NoError(t, err)
	require.Equal(t, original+2*watchTickInterval.Milliseconds(), next)
	require.Greater(t, next, f.now.UnixMilli(), "overdue intervals are skipped rather than backfilled")
}

func TestWatchBoundedQueueYieldsLargeTenantAfterOnePage(t *testing.T) {
	large := watchFixture(t, 77204)
	small := watchFixture(t, 77205)
	ctx := t.Context()
	large.now = small.now
	for i, f := range []*loyaltyClockFixture{large, small} {
		require.NoError(t, f.client.Do(ctx, f.client.B().Zadd().Key(loyaltyDueKey).ScoreMember().ScoreMember(float64(i), strconv.FormatUint(f.id, 10)).Build()).Error())
	}
	calls := []string{}
	large.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
		var req manage.ChattersRequest
		require.NoError(t, codec.Unmarshal(body, &req))
		calls = append(calls, req.BroadcasterID)
		reply := correlatedWatchReply(req)
		reply.CheckedAtUnixMilli = large.now.UnixMilli()
		reply.Live = true
		reply.Chatters = []manage.Chatter{{ID: "8"}}
		if req.BroadcasterID == strconv.FormatUint(large.id, 10) {
			reply.NextCursor = "large-next-page"
		} else {
			reply.Complete = true
		}
		b, err := codec.Marshal(reply)
		return &nats.Msg{Data: b}, err
	}
	jobs := make(chan uint64, 1)
	large.clock.queueDue(ctx, jobs)
	require.Len(t, jobs, 1, "full bounded queue returns without blocking")
	first := <-jobs
	require.Equal(t, large.id, first)
	large.clock.fire(ctx, first)
	require.Len(t, calls, 1, "large tenant yields after one page")
	require.Equal(t, "large-next-page", large.fields(t)["cursor"])
	large.clock.queueDue(ctx, jobs)
	require.Len(t, jobs, 1)
	second := <-jobs
	require.Equal(t, small.id, second, "small tenant remains eligible ahead of large continuation")
	large.clock.fire(ctx, second)
	require.Equal(t, []string{strconv.FormatUint(large.id, 10), strconv.FormatUint(small.id, 10)}, calls)
	require.Empty(t, small.fields(t)["window"])
	require.NotEmpty(t, large.fields(t)["window"])
	require.EqualValues(t, 1, large.operations(t))
	require.EqualValues(t, 1, small.operations(t))
}
