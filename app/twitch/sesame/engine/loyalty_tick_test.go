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
	_, err := s.confirmStream(ctx, id, owner, manage.ChattersReply{CheckedAtUnixMilli: at, StreamID: "stream-1", StreamStartedAtUnixMilli: 1_700_000_000_000})
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

func TestWatchTickMissedNotificationResumesDueWindowAndPagination(t *testing.T) {
	f := watchFixture(t, 77102)
	ctx := context.Background()
	calls := 0
	var window string
	f.clock.request = func(ctx context.Context, _ string, body []byte) (*nats.Msg, error) {
		var req manage.ChattersRequest
		require.NoError(t, codec.Unmarshal(body, &req))
		calls++
		require.Equal(t, strconv.FormatUint(f.id, 10), req.BroadcasterID)
		require.Greater(t, req.DeadlineUnixMilli, time.Now().UnixMilli())
		reply := correlatedWatchReply(req)
		if calls == 1 {
			window = req.WindowID
			require.Empty(t, req.Cursor)
			require.True(t, req.CheckLive)
			reply.CheckedAtUnixMilli = f.now.UnixMilli()
			reply.Live = true
			reply.NextCursor = "next"
			reply.Chatters = []manage.Chatter{{ID: "8", Login: "viewer"}, {ID: "8", Login: "viewer"}, {ID: "99"}, {ID: "invalid"}}
		} else {
			require.Equal(t, window, req.WindowID)
			require.Equal(t, "next", req.Cursor)
			require.False(t, req.CheckLive)
			reply.Complete = true
			reply.Chatters = []manage.Chatter{{ID: "9", Login: "other"}}
		}
		encoded, err := codec.Marshal(reply)
		return &nats.Msg{Data: encoded}, err
	}
	// No key expiry is delivered. Another instance discovers the persisted due
	// window without resetting it and the bounded due queue still finds it.
	before := f.fields(t)["due"]
	f.clock.Arm(ctx, f.id)
	require.Equal(t, before, f.fields(t)["due"])
	queue := make(chan uint64, loyaltyQueueSize)
	f.clock.queueDue(ctx, queue)
	require.Equal(t, f.id, <-queue)
	f.clock.fire(ctx, f.id)
	fields := f.fields(t)
	require.Equal(t, window, fields["window"])
	require.Equal(t, "next", fields["cursor"])
	require.Equal(t, "1", fields["chunk"])
	f.now = f.now.Add(time.Second)
	f.clock.fire(ctx, f.id)
	require.Equal(t, 2, calls)
	require.EqualValues(t, 2, f.operations(t))
	require.Empty(t, f.fields(t)["window"])
}

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

func TestWatchTickRejectsCorrelationAndCursorCycles(t *testing.T) {
	f := watchFixture(t, 77106)
	ctx := context.Background()
	f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
		var req manage.ChattersRequest
		require.NoError(t, codec.Unmarshal(body, &req))
		reply := correlatedWatchReply(req)
		reply.BroadcasterID = "wrong-tenant"
		encoded, err := codec.Marshal(reply)
		return &nats.Msg{Data: encoded}, err
	}
	f.clock.fire(ctx, f.id)
	require.Zero(t, f.operations(t))
	require.Equal(t, "1", f.fields(t)["failures"])
	require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(loyaltyClaimKey(f.id)).Build()).Error())
	f.due(t)
	require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key(loyaltyScheduleKey(f.id)).FieldValue().FieldValue("cursor_seen:cycled", "1").Build()).Error())
	f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
		var req manage.ChattersRequest
		require.NoError(t, codec.Unmarshal(body, &req))
		reply := correlatedWatchReply(req)
		reply.CheckedAtUnixMilli = f.now.UnixMilli()
		reply.Live = true
		reply.NextCursor = "cycled"
		reply.Chatters = []manage.Chatter{{ID: "8"}}
		encoded, err := codec.Marshal(reply)
		return &nats.Msg{Data: encoded}, err
	}
	f.clock.fire(ctx, f.id)
	require.Zero(t, f.operations(t))
	require.Contains(t, f.fields(t)["last_error"], "cursor cycle")
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

func TestWatchTickViewerFiltering(t *testing.T) {
	c := &ValkeyLoyaltyClock{botID: "99"}
	for _, raw := range []string{"99", "0", "bad"} {
		_, ok := c.chatterViewerID(raw)
		require.False(t, ok)
	}
	id, ok := c.chatterViewerID("8")
	require.True(t, ok)
	require.EqualValues(t, 8, id)
	require.EqualError(t, &chattersError{message: "failure"}, "failure")
}

func TestWatchTickPageContract(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong tenant", "wrong request", "wrong window", "wrong generation", "wrong session", "oversize", "future confirmation", "stale confirmation", "missing stream", "oversize stream", "missing stream start", "future stream start"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now()
			c := &ValkeyLoyaltyClock{chattersSubject: "test.chatters", now: func() time.Time { return now }}
			state := loyaltySchedule{generation: "7", liveSession: "2000", window: "42:2000:1234", cursor: "next"}
			c.request = func(ctx context.Context, subject string, body []byte) (*nats.Msg, error) {
				require.Equal(t, "test.chatters", subject)
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				require.Equal(t, "42", req.BroadcasterID)
				require.Equal(t, state.window, req.WindowID)
				require.Equal(t, state.cursor, req.Cursor)
				require.True(t, req.CheckLive)
				require.NotEmpty(t, req.RequestID)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.Equal(t, deadline.UnixMilli(), req.DeadlineUnixMilli)
				reply := correlatedWatchReply(req)
				reply.Live = true
				reply.CheckedAtUnixMilli = now.UnixMilli()
				reply.Complete = true
				switch scenario {
				case "missing stream":
					reply.StreamID = ""
				case "oversize stream":
					reply.StreamID = string(make([]byte, 129))
				case "missing stream start":
					reply.StreamStartedAtUnixMilli = 0
				case "future stream start":
					reply.StreamStartedAtUnixMilli = now.Add(time.Second).UnixMilli()
				case "wrong tenant":
					reply.BroadcasterID = "43"
				case "wrong request":
					reply.RequestID = "other"
				case "wrong window":
					reply.WindowID = "other"
				case "wrong generation":
					reply.SessionGeneration = "8"
				case "wrong session":
					reply.LiveSession = "3000"
				case "oversize":
					reply.Chatters = make([]manage.Chatter, 1001)
				case "future confirmation":
					reply.CheckedAtUnixMilli = now.Add(2 * time.Minute).UnixMilli()
				case "stale confirmation":
					reply.CheckedAtUnixMilli = now.Add(-2 * time.Minute).UnixMilli()
				}
				encoded, err := codec.Marshal(reply)
				return &nats.Msg{Data: encoded}, err
			}
			_, err := c.fetchPage(context.Background(), 42, state, true)
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
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

func TestWatchTickDefinitiveCursorErrorStartsFreshWindow(t *testing.T) {
	f := watchFixture(t, 77109)
	ctx := context.Background()
	calls := 0
	f.clock.request = func(_ context.Context, _ string, body []byte) (*nats.Msg, error) {
		var req manage.ChattersRequest
		require.NoError(t, codec.Unmarshal(body, &req))
		calls++
		reply := correlatedWatchReply(req)
		reply.CheckedAtUnixMilli, reply.Live = f.now.UnixMilli(), true
		switch calls {
		case 1:
			reply.NextCursor = "expired-cursor"
			reply.Chatters = []manage.Chatter{{ID: "8"}}
		case 2:
			require.Equal(t, "expired-cursor", req.Cursor)
			reply.Error, reply.ErrorCode = "cursor expired", "invalid"
		default:
			require.Empty(t, req.Cursor)
			reply.Complete = true
			reply.Chatters = []manage.Chatter{{ID: "9"}}
		}
		encoded, err := codec.Marshal(reply)
		return &nats.Msg{Data: encoded}, err
	}
	f.clock.fire(ctx, f.id)
	oldWindow := f.fields(t)["window"]
	f.now = f.now.Add(time.Second)
	f.clock.fire(ctx, f.id)
	require.Empty(t, f.fields(t)["window"])
	require.Equal(t, "1", f.fields(t)["active"])
	require.Equal(t, strconv.FormatInt(f.now.Add(watchTickInterval).UnixMilli(), 10), f.fields(t)["due"])
	f.now = f.now.Add(watchTickInterval + time.Second)
	f.clock.fire(ctx, f.id)
	require.Equal(t, 3, calls)
	require.EqualValues(t, 2, f.operations(t), "accepted chunks from abandoned window remain intact")
	require.Empty(t, f.fields(t)["window"])
	require.NotEmpty(t, oldWindow)
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

func TestWatchViewerSnapshotRequiresCompleteFirstPage(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(strconv.FormatBool(complete), func(t *testing.T) {
			f := watchFixture(t, uint64(8510000000)+uint64(len(strconv.FormatBool(complete))))
			key := chattersSnapshotKey(f.id)
			require.NoError(t, f.client.Do(t.Context(), f.client.B().Del().Key(key).Build()).Error())
			t.Cleanup(func() { f.client.Do(context.Background(), f.client.B().Del().Key(key).Build()) })
			f.clock.viewers = NewValkeyChatters(f.client, zap.NewNop())
			f.clock.request = func(ctx context.Context, _ string, body []byte) (*nats.Msg, error) {
				var req manage.ChattersRequest
				require.NoError(t, codec.Unmarshal(body, &req))
				reply := manage.ChattersReply{BroadcasterID: req.BroadcasterID, RequestID: req.RequestID, WindowID: req.WindowID, SessionGeneration: req.SessionGeneration, LiveSession: req.LiveSession, Complete: complete, Chatters: []manage.Chatter{{ID: "42", Login: "viewer"}}}
				if !complete {
					reply.NextCursor = "next"
				}
				reply.CheckedAtUnixMilli = f.now.UnixMilli()
				reply.Live = true
				reply.StreamID = "stream-one"
				reply.StreamStartedAtUnixMilli = f.now.Add(-time.Hour).UnixMilli()
				payload, err := codec.Marshal(reply)
				return &nats.Msg{Data: payload}, err
			}
			// First confirmation binds the actual stream identity, then a fresh
			// due window samples the first page within that stream.
			f.clock.fire(t.Context(), f.id)
			f.due(t)
			f.clock.fire(t.Context(), f.id)
			entries, ok, err := f.clock.viewers.Snapshot(t.Context(), f.id)
			require.NoError(t, err)
			require.Equal(t, complete, ok)
			if complete {
				require.Len(t, entries, 1)
				require.Equal(t, uint64(42), entries[0].ID)
			}
		})
	}
}
