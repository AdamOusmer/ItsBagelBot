// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gateStoreFixture struct {
	store *ValkeyTimerStore
	pub   *fakePublisher
	bid   uint64
}

func newGateStoreFixture(t *testing.T, proj projection.Reader) gateStoreFixture {
	t.Helper()
	client := newHotPathTestClient(t)
	pub := &fakePublisher{}
	bid := uint64(time.Now().UnixNano())
	store := NewValkeyTimerStore(client, pub, proj, fakeLive{live: true}, TimersConfig{OutgressStandardSubject: standardSubj})
	f := gateStoreFixture{store: store, pub: pub, bid: bid}
	t.Cleanup(func() {
		keys := []string{chatLog(bid).Key}
		for _, id := range []string{"t1", "capped", "ended", "plain", "off", "live"} {
			keys = append(keys, f.ref(id).scheduleKey(), f.ref(id).firesKey())
		}
		client.Do(context.Background(), client.B().Del().Key(keys...).Build())
	})
	return f
}

func (f gateStoreFixture) ref(timerID string) timerRef {
	return timerRef{broadcasterID: f.bid, id: timerID}
}

func (f gateStoreFixture) armed(timerID string, td timerDef) armedTimer {
	return armedTimer{ref: f.ref(timerID), def: td}
}

func (f gateStoreFixture) addLines(t *testing.T, n int, age time.Duration) {
	t.Helper()
	for range n {
		require.NoError(t, pkg_valkey.RecordRecent(context.Background(), f.store.client, chatLog(f.bid), time.Now().Add(-age)))
	}
}

func (f gateStoreFixture) scheduleKeyExists(t *testing.T, timerID string) bool {
	t.Helper()
	_, err := f.store.client.Do(context.Background(), f.store.client.B().Get().Key(f.ref(timerID).scheduleKey()).Build()).ToString()
	return err == nil
}

func TestTimerTickGateSkipReArmsWithoutFiring(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5}

	f.addLines(t, 4, time.Minute)

	f.store.tick(ctx, f.armed("t1", td))

	assert.Empty(t, f.pub.got, "a gate-skipped tick must not fire")
	assert.True(t, f.scheduleKeyExists(t, "t1"), "a skip must still re-arm at the exact interval (D8)")
}

func TestTimerTickGateSkipLeavesFireCapUntouched(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5, MaxFires: 3}

	f.addLines(t, 2, time.Minute)
	_, err := pkg_valkey.Incr(ctx, f.store.client, f.ref("t1").firesKey(), timerAuxTTL)
	require.NoError(t, err)

	f.store.tick(ctx, f.armed("t1", td))

	assert.Empty(t, f.pub.got, "a gate-skipped tick must not fire")
	assert.EqualValues(t, 1, f.store.fireCount(ctx, f.ref("t1")), "a skip must not consume the fire cap")
	assert.True(t, f.scheduleKeyExists(t, "t1"), "a skip must still re-arm")
}

func TestTimerTickGatePassFires(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5}

	f.addLines(t, 5, time.Minute)

	f.store.tick(ctx, f.armed("t1", td))

	assert.EqualValues(t, 1, f.store.fireCount(ctx, f.ref("t1")), "a fire must count toward the cap")
	assert.True(t, f.scheduleKeyExists(t, "t1"))
	assert.Eventually(t, func() bool { return len(f.pub.snapshot()) == 1 }, time.Second, time.Millisecond,
		"a gate-passed tick must fire")
}

func TestTimerTickCapReachedStopsWithoutFiringOrReArming(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MaxFires: 1}

	_, err := pkg_valkey.Incr(ctx, f.store.client, f.ref("t1").firesKey(), timerAuxTTL)
	require.NoError(t, err)

	f.store.tick(ctx, f.armed("t1", td))

	assert.Empty(t, f.pub.got, "a capped timer must not fire again")
	assert.False(t, f.scheduleKeyExists(t, "t1"), "a stop must not re-arm (§6 step 1)")
	assert.EqualValues(t, 1, f.store.fireCount(ctx, f.ref("t1")), "a stop must not itself bump the fire count")
}

func TestArmAllSkipsCappedAndEndedTimersButArmsAPlainOne(t *testing.T) {
	cfg := timersConfig{Timers: []timerDef{
		{ID: "capped", Message: "hi", Interval: 60, Enabled: true, MaxFires: 1},
		{ID: "ended", Message: "hi", Interval: 60, Enabled: true, EndsAt: "2020-01-01T00:00:00Z"},
		{ID: "plain", Message: "hi", Interval: 60, Enabled: true},
	}}
	blob, err := codec.Marshal(cfg)
	require.NoError(t, err)
	proj := fakeReader{modules: map[string]projection.ModuleView{
		timersModuleName: {Name: timersModuleName, IsEnabled: true, Configs: blob},
	}}
	f := newGateStoreFixture(t, proj)
	ctx := context.Background()

	_, err = pkg_valkey.Incr(ctx, f.store.client, f.ref("capped").firesKey(), timerAuxTTL)
	require.NoError(t, err)

	f.store.ArmAll(ctx, f.bid)

	assert.False(t, f.scheduleKeyExists(t, "capped"), "ArmAll must skip a capped timer (D5)")
	assert.False(t, f.scheduleKeyExists(t, "ended"), "ArmAll must skip an ended timer (D6)")
	assert.True(t, f.scheduleKeyExists(t, "plain"), "ArmAll must still arm an ungated, unstopped timer")
}

func TestTimerTickGateIgnoresLinesOlderThanTheWindow(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 3, ChatWindowMinutes: 5}

	f.addLines(t, 10, 6*time.Minute)
	f.addLines(t, 2, time.Minute)
	f.store.tick(ctx, f.armed("t1", td))

	assert.Empty(t, f.pub.snapshot(), "lines outside the window must not count")

	f.addLines(t, 1, 4*time.Minute)
	f.store.tick(ctx, f.armed("t1", td))

	assert.Eventually(t, func() bool { return len(f.pub.snapshot()) == 1 }, time.Second, time.Millisecond,
		"exactly the threshold inside the window passes")
}

func TestChatLogKeepsOnlyTheNewestHundred(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()

	f.addLines(t, maxGateLines+20, time.Second)

	n, err := f.store.client.Do(ctx, f.store.client.B().Zcard().Key(chatLog(f.bid).Key).Build()).AsInt64()
	require.NoError(t, err)
	assert.EqualValues(t, maxGateLines, n)
}

func (f gateStoreFixture) withConfig(t *testing.T, timers ...timerDef) {
	t.Helper()
	blob, err := codec.Marshal(timersConfig{Timers: timers})
	require.NoError(t, err)
	f.store.proj = fakeReader{modules: map[string]projection.ModuleView{
		timersModuleName: {Name: timersModuleName, IsEnabled: true, Configs: blob},
	}}
}

func offlineFixture(t *testing.T, timers ...timerDef) gateStoreFixture {
	f := newGateStoreFixture(t, fakeReader{})
	f.withConfig(t, timers...)
	f.store.live = fakeLive{live: false}
	return f
}

func TestOnExpiredOfflineTickFiresAndReArmsForAllowOfflineTimer(t *testing.T) {
	f := offlineFixture(t, timerDef{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true})

	f.store.onExpired(context.Background(), f.ref("off").scheduleKey())

	assert.Eventually(t, func() bool { return len(f.pub.snapshot()) == 1 }, time.Second, time.Millisecond)
	assert.True(t, f.scheduleKeyExists(t, "off"), "an offline timer must re-arm while offline")
}

func TestOnExpiredOfflineStopsLiveOnlyTimer(t *testing.T) {
	f := offlineFixture(t, timerDef{ID: "live", Message: "hi", Interval: 60, Enabled: true})

	f.store.onExpired(context.Background(), f.ref("live").scheduleKey())

	assert.Empty(t, f.pub.snapshot())
	assert.False(t, f.scheduleKeyExists(t, "live"), "a live-only timer must not re-arm while offline")
}

func TestRearmOfflineArmsOnlyAllowOfflineTimers(t *testing.T) {
	f := offlineFixture(t,
		timerDef{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true},
		timerDef{ID: "live", Message: "hi", Interval: 60, Enabled: true},
	)

	f.store.Rearm(context.Background(), f.bid)

	assert.True(t, f.scheduleKeyExists(t, "off"))
	assert.False(t, f.scheduleKeyExists(t, "live"))
}

func TestDisarmAllKeepsAllowOfflineTimerArmedAndResetsItsFireCount(t *testing.T) {
	f := offlineFixture(t,
		timerDef{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true, MaxFires: 1},
		timerDef{ID: "live", Message: "hi", Interval: 60, Enabled: true},
	)
	ctx := context.Background()
	f.store.ArmAll(ctx, f.bid)
	_, err := pkg_valkey.Incr(ctx, f.store.client, f.ref("off").firesKey(), timerAuxTTL)
	require.NoError(t, err)

	f.store.DisarmAll(ctx, f.bid)

	assert.True(t, f.scheduleKeyExists(t, "off"), "an offline timer must stay armed")
	assert.EqualValues(t, 0, f.store.fireCount(ctx, f.ref("off")), "the fire cap must reset for the offline stretch")
	assert.False(t, f.scheduleKeyExists(t, "live"), "a live-only timer must be disarmed")
}

func TestDisarmAllRearmsAllowOfflineTimerThatHitItsCap(t *testing.T) {
	f := offlineFixture(t, timerDef{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true, MaxFires: 1})
	ctx := context.Background()
	_, err := pkg_valkey.Incr(ctx, f.store.client, f.ref("off").firesKey(), timerAuxTTL)
	require.NoError(t, err)
	require.False(t, f.scheduleKeyExists(t, "off"))

	f.store.DisarmAll(ctx, f.bid)

	assert.True(t, f.scheduleKeyExists(t, "off"), "a capped offline timer must start again for the offline stretch")
}

func TestDisarmAllClearsScheduleAndFireKeys(t *testing.T) {
	cfg := timersConfig{Timers: []timerDef{
		{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5, MaxFires: 3},
	}}
	blob, err := codec.Marshal(cfg)
	require.NoError(t, err)
	proj := fakeReader{modules: map[string]projection.ModuleView{
		timersModuleName: {Name: timersModuleName, IsEnabled: true, Configs: blob},
	}}
	f := newGateStoreFixture(t, proj)
	ctx := context.Background()

	f.store.ArmAll(ctx, f.bid)
	f.addLines(t, 3, time.Second)
	_, err = pkg_valkey.Incr(ctx, f.store.client, f.ref("t1").firesKey(), timerAuxTTL)
	require.NoError(t, err)
	require.True(t, f.scheduleKeyExists(t, "t1"), "precondition: timer must be armed before disarming it")

	f.store.DisarmAll(ctx, f.bid)

	assert.False(t, f.scheduleKeyExists(t, "t1"), "DisarmAll must delete the schedule key")
	assert.EqualValues(t, 0, f.store.fireCount(ctx, f.ref("t1")), "DisarmAll must delete the fire count")
}

func TestArmOnlineResetsCappedAllowOfflineTimer(t *testing.T) {
	td := timerDef{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true, MaxFires: 1}
	f := offlineFixture(t, td)
	ctx := context.Background()
	f.store.tick(ctx, f.armed("off", td))
	f.store.delAuxKey(ctx, f.ref("off"), f.ref("off").scheduleKey())
	f.store.tick(ctx, f.armed("off", td))
	require.EqualValues(t, 1, f.store.fireCount(ctx, f.ref("off")))
	require.False(t, f.scheduleKeyExists(t, "off"), "precondition: capped timer is stopped")

	f.store.ArmOnline(ctx, f.bid)

	assert.EqualValues(t, 0, f.store.fireCount(ctx, f.ref("off")))
	assert.True(t, f.scheduleKeyExists(t, "off"))
}

func TestRearmWhileLiveDoesNotResetCappedTimer(t *testing.T) {
	f := offlineFixture(t, timerDef{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true, MaxFires: 1})
	f.store.live = fakeLive{live: true}
	ctx := context.Background()
	_, err := pkg_valkey.Incr(ctx, f.store.client, f.ref("off").firesKey(), timerAuxTTL)
	require.NoError(t, err)

	f.store.Rearm(ctx, f.bid)

	assert.EqualValues(t, 1, f.store.fireCount(ctx, f.ref("off")))
	assert.False(t, f.scheduleKeyExists(t, "off"))
}

func (f gateStoreFixture) chatLogSize(t *testing.T) int64 {
	t.Helper()
	n, err := f.store.client.Do(context.Background(), f.store.client.B().Zcard().Key(chatLog(f.bid).Key).Build()).AsInt64()
	require.NoError(t, err)
	return n
}

func TestCountChatLineFeedsTheTimersThatNeedIt(t *testing.T) {
	cases := []struct {
		name       string
		timers     []timerDef
		wantLogged int64
		wantArmed  bool
	}{
		{
			name:       "a gated timer counts the chat line",
			timers:     []timerDef{{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 2}},
			wantLogged: 3,
		},
		{
			name:      "an allow-offline timer is rearmed once for the whole burst",
			timers:    []timerDef{{ID: "off", Message: "hi", Interval: 60, Enabled: true, AllowOffline: true}},
			wantArmed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := offlineFixture(t, tc.timers...)
			reads := &countingReader{fakeReader: f.store.proj.(fakeReader)}
			f.store.proj = reads
			ctx := context.Background()

			for range 3 {
				f.store.CountChatLine(ctx, f.bid)
			}

			assert.Eventually(t, func() bool {
				return f.chatLogSize(t) == tc.wantLogged && f.scheduleKeyExists(t, "off") == tc.wantArmed
			}, 2*time.Second, 5*time.Millisecond)
			assert.Never(t, func() bool { return reads.moduleReads > 2 }, 100*time.Millisecond, 10*time.Millisecond,
				"one config read and at most one rearm read, however many lines arrive")
		})
	}
}

func TestCountChatLineIgnoresTimersThatNeedNothing(t *testing.T) {
	f := offlineFixture(t,
		timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: false, MinChatLines: 2, AllowOffline: true},
		timerDef{ID: "plain", Message: "hi", Interval: 60, Enabled: true},
	)

	f.store.CountChatLine(context.Background(), f.bid)
	f.store.CountChatLine(context.Background(), 0)

	assert.Never(t, func() bool {
		return f.chatLogSize(t) > 0 || f.scheduleKeyExists(t, "plain") || f.scheduleKeyExists(t, "t1")
	}, 150*time.Millisecond, 10*time.Millisecond, "neither a disabled nor an ungated timer wants the line")
}
