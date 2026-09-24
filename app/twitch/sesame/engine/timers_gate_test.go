// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
	store := &ValkeyTimerStore{
		client:           client,
		pub:              pub,
		proj:             proj,
		live:             fakeLive{live: true},
		outgressStandard: standardSubj,
		log:              zap.NewNop(),
		now:              time.Now,
	}
	f := gateStoreFixture{store: store, pub: pub, bid: bid}
	t.Cleanup(func() {
		ctx := context.Background()
		client.Do(ctx, client.B().Del().Key(
			f.ref("t1").scheduleKey(), f.ref("t1").markKey(), f.ref("t1").firesKey(), linesKey(bid),
			f.ref("capped").scheduleKey(), f.ref("capped").markKey(), f.ref("capped").firesKey(),
			f.ref("ended").scheduleKey(), f.ref("ended").markKey(), f.ref("ended").firesKey(),
			f.ref("plain").scheduleKey(),
		).Build())
	})
	return f
}

func (f gateStoreFixture) ref(timerID string) timerRef {
	return timerRef{broadcasterID: f.bid, id: timerID}
}

func (f gateStoreFixture) armed(timerID string, td timerDef) armedTimer {
	return armedTimer{ref: f.ref(timerID), def: td}
}

func (f gateStoreFixture) bumpLines(t *testing.T, n int) {
	t.Helper()
	for range n {
		_, err := pkg_valkey.Incr(context.Background(), f.store.client, linesKey(f.bid), timerAuxTTL)
		require.NoError(t, err)
	}
}

func (f gateStoreFixture) seedMark(t *testing.T, timerID string, value int64) {
	t.Helper()
	err := f.store.client.Do(context.Background(), f.store.client.B().Set().
		Key(f.ref(timerID).markKey()).Value(strconv.FormatInt(value, 10)).Nx().Ex(timerAuxTTL).Build()).Error()
	require.NoError(t, err)
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

	f.seedMark(t, "t1", 0)
	f.bumpLines(t, 2)

	f.store.tick(ctx, f.armed("t1", td))

	assert.Empty(t, f.pub.got, "a gate-skipped tick must not fire")
	assert.EqualValues(t, 0, f.store.watermark(ctx, f.ref("t1")), "a skip must leave the watermark untouched (D4)")
	assert.True(t, f.scheduleKeyExists(t, "t1"), "a skip must still re-arm at the exact interval (D8)")
}

func TestTimerTickGateSkipLeavesFireCapUntouched(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5, MaxFires: 3}

	f.seedMark(t, "t1", 0)
	f.bumpLines(t, 2)
	_, err := pkg_valkey.Incr(ctx, f.store.client, f.ref("t1").firesKey(), timerAuxTTL)
	require.NoError(t, err)

	f.store.tick(ctx, f.armed("t1", td))

	assert.Empty(t, f.pub.got, "a gate-skipped tick must not fire")
	assert.EqualValues(t, 1, f.store.fireCount(ctx, f.ref("t1")), "a skip must not consume the fire cap")
	assert.True(t, f.scheduleKeyExists(t, "t1"), "a skip must still re-arm")
}

func TestTimerTickGatePassFiresAndMovesWatermark(t *testing.T) {
	f := newGateStoreFixture(t, fakeReader{})
	ctx := context.Background()
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5}

	f.seedMark(t, "t1", 0)
	f.bumpLines(t, 7)

	f.store.tick(ctx, f.armed("t1", td))

	assert.EqualValues(t, 7, f.store.watermark(ctx, f.ref("t1")), "a fire must move the watermark to the current counter (D4)")
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

func TestArmAllRearmDoesNotResetAnAlreadyMovedWatermark(t *testing.T) {
	td := timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true, MinChatLines: 5}
	cfg := timersConfig{Timers: []timerDef{td}}
	blob, err := codec.Marshal(cfg)
	require.NoError(t, err)
	proj := fakeReader{modules: map[string]projection.ModuleView{
		timersModuleName: {Name: timersModuleName, IsEnabled: true, Configs: blob},
	}}
	f := newGateStoreFixture(t, proj)
	ctx := context.Background()

	f.store.ArmAll(ctx, f.bid)
	require.EqualValues(t, 0, f.store.watermark(ctx, f.ref("t1")))

	f.bumpLines(t, 7)
	f.store.tick(ctx, f.armed("t1", td))
	require.EqualValues(t, 7, f.store.watermark(ctx, f.ref("t1")))
	require.Eventually(t, func() bool { return len(f.pub.snapshot()) == 1 }, time.Second, time.Millisecond)

	f.store.ArmAll(ctx, f.bid)

	assert.EqualValues(t, 7, f.store.watermark(ctx, f.ref("t1")),
		"a rearm must not reset an already-moved watermark (D4 NX guarantee)")
}

func TestDisarmAllClearsScheduleAndAuxKeys(t *testing.T) {
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
	f.bumpLines(t, 3)
	_, err = pkg_valkey.Incr(ctx, f.store.client, f.ref("t1").firesKey(), timerAuxTTL)
	require.NoError(t, err)
	require.True(t, f.scheduleKeyExists(t, "t1"), "precondition: timer must be armed before disarming it")

	f.store.DisarmAll(ctx, f.bid)

	assert.False(t, f.scheduleKeyExists(t, "t1"), "DisarmAll must delete the schedule key")
	assert.EqualValues(t, 0, f.store.watermark(ctx, f.ref("t1")), "DisarmAll must delete the watermark")
	assert.EqualValues(t, 0, f.store.fireCount(ctx, f.ref("t1")), "DisarmAll must delete the fire count")
	assert.EqualValues(t, 0, f.store.linesCount(ctx, f.bid), "DisarmAll must delete the broadcaster's chat line counter")
}

func TestTimerDefDecodesLegacyBlobAsUngatedAndUnstopped(t *testing.T) {
	var td timerDef
	require.NoError(t, codec.Unmarshal(
		[]byte(`{"id":"t1","message":"hi","intervalSeconds":60,"enabled":true}`), &td))

	assert.Equal(t, timerDef{ID: "t1", Message: "hi", Interval: 60, Enabled: true}, td)
	assert.False(t, isGated(td), "a legacy blob must decode with no gate")
	assert.False(t, stopped(td, 0, time.Now()), "a legacy blob must decode with no stop")
	assert.True(t, gatePasses(td, 0, 0), "an ungated timer's gate always passes")
}
