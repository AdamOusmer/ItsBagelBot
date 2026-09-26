// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

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

func TestWatchTickPageContract(t *testing.T) {
	now := time.Now()
	state := loyaltySchedule{generation: "7", liveSession: "2000", window: "42:2000:1234", cursor: "next"}
	for _, scenario := range pageContractScenarios(now) {
		t.Run(scenario.name, func(t *testing.T) {
			clock := pageContractClock(t, now, state, scenario.mutate)
			_, err := clock.fetchPage(context.Background(), 42, state, true)
			if scenario.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

type pageContractScenario struct {
	name      string
	wantError bool
	mutate    func(*manage.ChattersReply, time.Time)
}

func pageContractScenarios(now time.Time) []pageContractScenario {
	return []pageContractScenario{
		{name: "valid"},
		{name: "wrong tenant", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.BroadcasterID = "43" }},
		{name: "wrong request", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.RequestID = "other" }},
		{name: "wrong window", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.WindowID = "other" }},
		{name: "wrong generation", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.SessionGeneration = "8" }},
		{name: "wrong session", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.LiveSession = "3000" }},
		{name: "oversize", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.Chatters = make([]manage.Chatter, 1001) }},
		{name: "future confirmation", wantError: true, mutate: func(r *manage.ChattersReply, now time.Time) {
			r.CheckedAtUnixMilli = now.Add(2 * time.Minute).UnixMilli()
		}},
		{name: "stale confirmation", wantError: true, mutate: func(r *manage.ChattersReply, now time.Time) {
			r.CheckedAtUnixMilli = now.Add(-2 * time.Minute).UnixMilli()
		}},
		{name: "missing stream", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.StreamID = "" }},
		{name: "oversize stream", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.StreamID = strings.Repeat("x", 129) }},
		{name: "missing stream start", wantError: true, mutate: func(r *manage.ChattersReply, _ time.Time) { r.StreamStartedAtUnixMilli = 0 }},
		{name: "future stream start", wantError: true, mutate: func(r *manage.ChattersReply, now time.Time) {
			r.StreamStartedAtUnixMilli = now.Add(time.Second).UnixMilli()
		}},
	}
}

func pageContractClock(t *testing.T, now time.Time, state loyaltySchedule, mutate func(*manage.ChattersReply, time.Time)) *ValkeyLoyaltyClock {
	t.Helper()
	clock := &ValkeyLoyaltyClock{chattersSubject: "test.chatters", now: func() time.Time { return now }}
	clock.request = func(ctx context.Context, subject string, body []byte) (*nats.Msg, error) {
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
		if mutate != nil {
			mutate(&reply, now)
		}
		encoded, err := codec.Marshal(reply)
		return &nats.Msg{Data: encoded}, err
	}
	return clock
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
