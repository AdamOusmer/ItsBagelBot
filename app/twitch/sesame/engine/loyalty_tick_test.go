// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

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
	keys := []string{"settings:" + sid, "live:" + sid, livekey.VerKey(id), watchtime.AdmissionKey(id), loyaltyScheduleKey(id), loyaltyClaimKey(id), "watchtime:operations:" + sid, loyaltyTickKey(id)}
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
					require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key(livekey.Key(f.id)).Value("3000").Build()).Error())
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

// interceptLoyaltyArm applies an authoritative live transition exactly after
// Capture (and the recovery GET) but before the real arm Lua executes.
type interceptLoyaltyArm struct {
	valkey.Client
	before func()
}

func (c *interceptLoyaltyArm) Do(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
	if isLoyaltyArm(cmd) && c.before != nil {
		before := c.before
		c.before = nil
		before()
	}
	return c.Client.Do(ctx, cmd)
}

func isLoyaltyArm(cmd valkey.Completed) bool {
	args := cmd.Commands()
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "EVAL":
		return args[1] == loyaltyArmScript
	default:
		return false
	}
}

func (f *loyaltyClockFixture) clearSchedule(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.client.Do(ctx, f.client.B().Del().Key(loyaltyScheduleKey(f.id), loyaltyClaimKey(f.id)).Build()).Error())
	require.NoError(t, f.client.Do(ctx, f.client.B().Zrem().Key(loyaltyDueKey).Member(strconv.FormatUint(f.id, 10)).Build()).Error())
}
func (f *loyaltyClockFixture) requireUnscheduled(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	n, err := f.client.Do(ctx, f.client.B().Exists().Key(loyaltyScheduleKey(f.id)).Build()).AsInt64()
	require.NoError(t, err)
	require.Zero(t, n, "denied arm must not create an active schedule")
	err = f.client.Do(ctx, f.client.B().Zscore().Key(loyaltyDueKey).Member(strconv.FormatUint(f.id, 10)).Build()).Error()
	require.True(t, valkey.IsValkeyNil(err), "denied arm must not enter due queue")
}

func TestWatchTickHydratedOfflineAccountCannotArm(t *testing.T) {
	f := watchFixture(t, 77121)
	ctx := context.Background()
	f.clearSchedule(t)
	applied, err := clearLiveKey(ctx, f.client, f.id, 3000)
	require.NoError(t, err)
	require.True(t, applied)
	// Settings remain completely hydrated and loyalty-enabled; neither recovery
	// nor a late online arm can substitute settings for an authoritative live key.
	f.clock.Arm(ctx, f.id)
	f.clock.ArmVersioned(ctx, f.id, 2000)
	f.requireUnscheduled(t)
	// A later hydration refresh can update settings while offline as well.
	require.NoError(t, f.client.Do(ctx, f.client.B().Hset().Key("settings:"+strconv.FormatUint(f.id, 10)).FieldValue().FieldValue("active", "1").FieldValue("module:loyalty:enabled", "1").FieldValue("module:loyalty:config", `{"watchPointsPerTick":11}`).Build()).Error())
	f.clock.Arm(ctx, f.id)
	f.requireUnscheduled(t)
	// A real newer online event still admits the hydrated account.
	n, err := setLiveScript.Exec(ctx, f.client, []string{livekey.Key(f.id), livekey.VerKey(f.id)}, []string{"4000", "3600", "172800"}).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.clock.ArmVersioned(ctx, f.id, 4000)
	require.Equal(t, "1", f.fields(t)["active"])
	require.Equal(t, "4000", f.fields(t)["live_session"])
}

func TestWatchTickOnlineArmsWhenAnotherWriterStampedLive(t *testing.T) {
	f := watchFixture(t, 77125)
	ctx := context.Background()
	f.clearSchedule(t)
	n, err := setLiveScript.Exec(ctx, f.client, []string{livekey.Key(f.id), livekey.VerKey(f.id)}, []string{"2500", "3600", "172800"}).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.clock.ArmVersioned(ctx, f.id, 2000)
	fields := f.fields(t)
	require.Equal(t, "1", fields["active"])
	require.Equal(t, "2000", fields["version"])
	require.Equal(t, "2000", fields["live_session"])
	snap, allowed, err := f.clock.awards.Capture(ctx, f.id)
	require.NoError(t, err)
	require.True(t, allowed)
	require.True(t, loyaltySchedule{generation: fields["generation"], liveSession: fields["live_session"]}.matchesAdmission(snap))
}

func TestWatchTickStaleOnlineStaysRefusedAfterOfflineAndNewerLive(t *testing.T) {
	f := watchFixture(t, 77126)
	ctx := context.Background()
	f.clock.DisarmVersioned(ctx, f.id, 3000)
	applied, err := clearLiveKey(ctx, f.client, f.id, 3000)
	require.NoError(t, err)
	require.True(t, applied)
	n, err := setLiveScript.Exec(ctx, f.client, []string{livekey.Key(f.id), livekey.VerKey(f.id)}, []string{"5000", "3600", "172800"}).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.clock.ArmVersioned(ctx, f.id, 2000)
	fields := f.fields(t)
	require.Equal(t, "0", fields["active"])
	require.Equal(t, "3000", fields["version"])
}

func TestWatchTickOfflineBetweenAdmissionAndArmIsFenced(t *testing.T) {
	for _, online := range []bool{false, true} {
		t.Run(strconv.FormatBool(online), func(t *testing.T) {
			f := watchFixture(t, 77122)
			ctx := context.Background()
			f.clearSchedule(t)
			f.clock.client = &interceptLoyaltyArm{Client: f.client, before: func() {
				applied, err := clearLiveKey(ctx, f.client, f.id, 3000)
				require.NoError(t, err)
				require.True(t, applied)
			}}
			if online {
				f.clock.ArmVersioned(ctx, f.id, 2000)
			} else {
				f.clock.Arm(ctx, f.id)
			}
			f.requireUnscheduled(t)
		})
	}
}

func TestWatchTickLiveRecheckPreservesActiveWindow(t *testing.T) {
	f := watchFixture(t, 77123)
	ctx := context.Background()
	before := f.fields(t)
	n, err := setLiveScript.Exec(ctx, f.client, []string{livekey.Key(f.id), livekey.VerKey(f.id)}, []string{"5000", "3600", "172800"}).AsInt64()
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	f.clock.Arm(ctx, f.id)
	require.Equal(t, before, f.fields(t), "live rechecks must preserve the stable active schedule session and due time")
	// An older online event cannot replace that schedule even while live.
	f.clock.ArmVersioned(ctx, f.id, 3000)
	require.Equal(t, before, f.fields(t))
}

func TestWatchTickNewerLiveBetweenAdmissionAndArmIsFenced(t *testing.T) {
	for _, online := range []bool{false, true} {
		t.Run(strconv.FormatBool(online), func(t *testing.T) {
			f := watchFixture(t, 77124)
			ctx := context.Background()
			f.clearSchedule(t)
			require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key(loyaltyClaimKey(f.id)).Value("existing-claim").Build()).Error())
			f.clock.client = &interceptLoyaltyArm{Client: f.client, before: func() {
				n, err := setLiveScript.Exec(ctx, f.client, []string{livekey.Key(f.id), livekey.VerKey(f.id)}, []string{"5000", "3600", "172800"}).AsInt64()
				require.NoError(t, err)
				require.EqualValues(t, 1, n)
			}}
			if online {
				f.clock.ArmVersioned(ctx, f.id, 2000)
			} else {
				f.clock.Arm(ctx, f.id)
			}
			// An admission captured against 2000 cannot create a stale schedule after
			// authoritative live state moves to 5000, or delete existing worker claims.
			f.requireUnscheduled(t)
			claim, err := f.client.Do(ctx, f.client.B().Get().Key(loyaltyClaimKey(f.id)).Build()).ToString()
			require.NoError(t, err)
			require.Equal(t, "existing-claim", claim)
			// Recovery rereads current live state and can admit the legitimate session.
			f.clock.Arm(ctx, f.id)
			fields := f.fields(t)
			require.Equal(t, "1", fields["active"])
			require.Equal(t, "5000", fields["version"])
			require.Equal(t, "5000", fields["live_session"])
			score, err := f.client.Do(ctx, f.client.B().Zscore().Key(loyaltyDueKey).Member(strconv.FormatUint(f.id, 10)).Build()).ToString()
			require.NoError(t, err)
			require.Equal(t, fields["due"], score)
		})
	}
}
