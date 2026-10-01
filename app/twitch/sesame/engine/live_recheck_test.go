// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	recheckSystemSubject = "test.outgress.system"
	recheckLiveSubject   = "test.broadcaster.live.get"
)

func offlineConfirmKey(stage int, id uint64) string {
	return "live:confirm:" + strconv.Itoa(stage) + ":" + strconv.FormatUint(id, 10)
}

type recheckFixture struct {
	client valkey.Client
	pub    *fakePublisher
	store  *ValkeyLiveStore
	id     uint64
}

func newRecheckFixture(t *testing.T, nc *nats.Conn, confirmDelays ...time.Duration) *recheckFixture {
	t.Helper()
	client := newHotPathTestClient(t)
	id := uint64(time.Now().UnixNano())
	f := &recheckFixture{client: client, pub: &fakePublisher{}, id: id}
	f.store = NewValkeyLiveStore(client, nc, f.pub, LiveConfig{
		TTL:                   time.Minute,
		ProjectorLiveSubject:  recheckLiveSubject,
		OutgressSystemSubject: recheckSystemSubject,
		OfflineConfirmDelays:  confirmDelays,
		Log:                   zap.NewNop(),
	})
	t.Cleanup(f.store.Close)
	t.Cleanup(func() {
		idStr := strconv.FormatUint(id, 10)
		keys := []string{liveKey(id), livekey.VerKey(id), recheckKeyPrefix + idStr, demandRecheckKeyPrefix + idStr}
		for stage := range confirmDelays {
			keys = append(keys, offlineConfirmKey(stage, id))
		}
		client.Do(context.Background(), client.B().Del().Key(keys...).Build())
	})
	return f
}

func (f *recheckFixture) rechecks() []outgress.Message {
	want := captured{subject: recheckSystemSubject, msg: outgress.Message{
		Type:          outgress.TypeStreamStatus,
		BroadcasterID: strconv.FormatUint(f.id, 10),
	}}
	var jobs []outgress.Message
	for _, c := range f.pub.snapshot() {
		got := captured{subject: c.subject, msg: outgress.Message{Type: c.msg.Type, BroadcasterID: c.msg.BroadcasterID}}
		if assert.ObjectsAreEqual(want, got) {
			jobs = append(jobs, c.msg)
		}
	}
	return jobs
}

func (f *recheckFixture) exists(t *testing.T, key string) bool {
	t.Helper()
	n, err := f.client.Do(context.Background(), f.client.B().Exists().Key(key).Build()).AsInt64()
	require.NoError(t, err)
	return n == 1
}

func liveRPCResponder(t *testing.T, reply func() projectorrpc.LiveReply) *nats.Conn {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	_, err = nc.Subscribe(recheckLiveSubject, func(msg *nats.Msg) {
		body, err := codec.Marshal(reply())
		if err != nil {
			return
		}
		_ = msg.Respond(body)
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	return nc
}

func startedRecheckFixture(t *testing.T, confirmDelays ...time.Duration) *recheckFixture {
	t.Helper()
	f := newRecheckFixture(t, nil, confirmDelays...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.store.StartExpiryWatcher(ctx)
	time.Sleep(200 * time.Millisecond)
	return f
}

func TestOfflineRightAfterOnlineIsRecheckedAgainstTwitch(t *testing.T) {
	f := startedRecheckFixture(t, 800*time.Millisecond, 1600*time.Millisecond)
	ctx := context.Background()

	online := livekey.VersionNow()
	applied, err := f.store.SetLive(ctx, f.id, online)
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = f.store.ClearLive(ctx, f.id, online+399)
	require.NoError(t, err)
	require.True(t, applied, "the later-stamped offline wins the version check")

	require.Eventually(t, func() bool { return len(f.rechecks()) == 1 }, 5*time.Second, 50*time.Millisecond,
		"an applied offline must be confirmed against Twitch")

	twitchSaysLive, err := f.store.setLiveKey(ctx, f.id, livekey.VersionNow())
	require.NoError(t, err)
	require.True(t, twitchSaysLive, "a confirmation that finds the stream live must outrank the offline")
	live, err := f.store.IsLive(ctx, f.id)
	require.NoError(t, err)
	assert.True(t, live)

	require.Never(t, func() bool { return len(f.rechecks()) > 1 }, 3*time.Second, 50*time.Millisecond,
		"a recovered stream needs no second confirmation")
}

func TestOfflineStillUnconfirmedIsRecheckedAgain(t *testing.T) {
	f := startedRecheckFixture(t, 500*time.Millisecond, 1500*time.Millisecond)

	applied, err := f.store.ClearLive(context.Background(), f.id, livekey.VersionNow())
	require.NoError(t, err)
	require.True(t, applied)

	require.Eventually(t, func() bool { return len(f.rechecks()) == 2 }, 10*time.Second, 50*time.Millisecond,
		"Twitch may not list a just-started stream at the first confirmation")
}

func TestSupersededOfflineArmsNoConfirmation(t *testing.T) {
	f := newRecheckFixture(t, nil, time.Minute, time.Hour)
	ctx := context.Background()

	applied, err := f.store.SetLive(ctx, f.id, 2000)
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = f.store.ClearLive(ctx, f.id, 1000)
	require.NoError(t, err)
	require.False(t, applied)

	assert.False(t, f.exists(t, offlineConfirmKey(0, f.id)))
	assert.False(t, f.exists(t, offlineConfirmKey(1, f.id)))
	assert.True(t, f.exists(t, liveKey(f.id)), "a stale offline must leave the live key alone")
}

func TestExpiredKeysThatRecheck(t *testing.T) {
	tests := []struct {
		name string
		key  func(id uint64) string
		want int
	}{
		{name: "live key", key: liveKey, want: 1},
		{name: "first offline confirmation", key: func(id uint64) string { return offlineConfirmKey(0, id) }, want: 1},
		{name: "second offline confirmation", key: func(id uint64) string { return offlineConfirmKey(1, id) }, want: 1},
		{name: "version claim", key: livekey.VerKey, want: 0},
		{name: "recheck claim", key: func(id uint64) string { return recheckKeyPrefix + strconv.FormatUint(id, 10) }, want: 0},
		{name: "demand claim", key: func(id uint64) string { return demandRecheckKeyPrefix + strconv.FormatUint(id, 10) }, want: 0},
		{name: "unrelated key", key: func(id uint64) string { return "timer:" + strconv.FormatUint(id, 10) + ":a" }, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRecheckFixture(t, nil, time.Minute, time.Hour)
			f.store.onExpired(context.Background(), tt.key(f.id))
			assert.Len(t, f.rechecks(), tt.want)
		})
	}
}

func TestConfirmationIsSkippedOnceLiveAgain(t *testing.T) {
	f := newRecheckFixture(t, nil, time.Minute, time.Hour)
	ctx := context.Background()

	applied, err := f.store.ClearLive(ctx, f.id, 1000)
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = f.store.SetLive(ctx, f.id, 2000)
	require.NoError(t, err)
	require.True(t, applied)

	f.store.onExpired(ctx, offlineConfirmKey(0, f.id))

	assert.Empty(t, f.rechecks(), "a restarted stream must not be checked against a lagging Twitch answer")
}

func TestOfflineEventDefersDemandRecheck(t *testing.T) {
	nc := liveRPCResponder(t, func() projectorrpc.LiveReply {
		return projectorrpc.LiveReply{Live: false, Known: true}
	})
	f := newRecheckFixture(t, nc, time.Minute, time.Hour)
	ctx := context.Background()

	applied, err := f.store.ClearLive(ctx, f.id, livekey.VersionNow())
	require.NoError(t, err)
	require.True(t, applied)

	live, err := f.store.IsLive(ctx, f.id)
	require.NoError(t, err)
	assert.False(t, live)
	assert.Empty(t, f.rechecks(), "the scheduled confirmation owns the first check after an offline event")
}

func TestExpiryRechecksAreClaimedOncePerWindow(t *testing.T) {
	f := newRecheckFixture(t, nil, time.Minute, time.Hour)
	ctx := context.Background()

	f.store.onExpired(ctx, offlineConfirmKey(0, f.id))
	f.store.onExpired(ctx, offlineConfirmKey(0, f.id))

	assert.Len(t, f.rechecks(), 1, "replicas seeing the same expiry must publish one recheck")
}

func TestColdReadOfProjectedLiveState(t *testing.T) {
	tests := []struct {
		name         string
		reply        projectorrpc.LiveReply
		wantLive     bool
		wantRechecks int
	}{
		{name: "projected offline is verified against Twitch once per window", reply: projectorrpc.LiveReply{Known: true}, wantRechecks: 1},
		{name: "unknown is left to the projector escalation", reply: projectorrpc.LiveReply{}},
		{name: "projected live restores the key", reply: projectorrpc.LiveReply{Live: true, Known: true}, wantLive: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := liveRPCResponder(t, func() projectorrpc.LiveReply { return tt.reply })
			f := newRecheckFixture(t, nc)
			ctx := context.Background()

			for range 2 {
				f.store.cache.Invalidate(f.id)
				live, err := f.store.IsLive(ctx, f.id)
				require.NoError(t, err)
				assert.Equal(t, tt.wantLive, live)
			}

			assert.Len(t, f.rechecks(), tt.wantRechecks)
			assert.Equal(t, tt.wantLive, f.exists(t, liveKey(f.id)))
		})
	}
}
