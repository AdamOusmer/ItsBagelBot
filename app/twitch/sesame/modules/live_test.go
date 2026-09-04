// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	lane "ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The #561 regression tests: a rapid stream.online / stream.offline pair must
// leave the broadcaster offline, timers disarmed and greets untouched by any
// superseded online — whichever order the events arrive in.

// versionedLive is a recording LiveStore applying the same
// newer-version-wins rule as the Valkey scripts, so tests can drive both
// arrival orders through realistic store semantics. Applied transitions are
// appended to a shared lifecycle log for order assertions.
type versionedLive struct {
	mu      sync.Mutex
	applied int64 // highest version applied so far; survives deletion like ver:
	isLive  bool

	setCalls   []int64 // every call, applied or not
	clearCalls []int64

	log *lifecycleLog
}

func (f *versionedLive) IsLive(context.Context, uint64) (bool, error) { return f.isLive, nil }

// claim applies one transition under the same newer-version-wins rule the
// Valkey scripts use; both wrappers differ only in which call log they record.
// Everything touches f's fields under f.mu, including the recording: the pump
// goroutine runs these while the test goroutine reads them.
func (f *versionedLive) claim(calls *[]int64, version int64, name string, liveAfter bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*calls = append(*calls, version)
	if version < f.applied {
		return false, nil
	}
	f.applied = version
	f.isLive = liveAfter
	f.log.add(name + ":" + strconv.FormatInt(version, 10))
	return true, nil
}

func (f *versionedLive) SetLive(_ context.Context, _ uint64, version int64) (bool, error) {
	return f.claim(&f.setCalls, version, "set", true)
}

func (f *versionedLive) ClearLive(_ context.Context, _ uint64, version int64) (bool, error) {
	return f.claim(&f.clearCalls, version, "clear", false)
}

// orderedTimers and orderedGreets funnel their calls into the shared log.
type orderedTimers struct{ log *lifecycleLog }

func (t orderedTimers) ArmAll(context.Context, uint64)    { t.log.add("arm") }
func (t orderedTimers) DisarmAll(context.Context, uint64) { t.log.add("disarm") }

type orderedGreets struct{ log *lifecycleLog }

func (g orderedGreets) FirstGreet(context.Context, uint64, string) (bool, error) { return false, nil }
func (g orderedGreets) ResetGreets(context.Context, uint64) error {
	g.log.add("greets")
	return nil
}

// lifecycleLog is the one ordered record every fake appends to.
type lifecycleLog struct {
	mu    sync.Mutex
	items []string
}

func (l *lifecycleLog) add(item string) {
	l.mu.Lock()
	l.items = append(l.items, item)
	l.mu.Unlock()
}

func (l *lifecycleLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.items...)
}

func (l *lifecycleLog) reset() {
	l.mu.Lock()
	l.items = nil
	l.mu.Unlock()
}

func waitForLog(t *testing.T, l *lifecycleLog, n int) []string {
	t.Helper()
	assert.Eventually(t, func() bool { return len(l.snapshot()) >= n }, time.Second, time.Millisecond,
		"expected %d lifecycle entries, got %v", n, l.snapshot())
	return l.snapshot()
}

// millis renders an RFC3339 instant the way EventVersion does, so expected
// versions stay readable without hardcoding epoch math.
func millis(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return strconv.FormatInt(t.UnixMilli(), 10)
}

func liveCtx(eventType, receivedAt string) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:              eventType,
			BroadcasterUserID: "2",
			ReceivedAt:        receivedAt,
		},
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
}

func runLifecycle(t *testing.T, m module.Module, eventType, ts string) {
	t.Helper()
	h := m.Events[eventType]
	require.NotNil(t, h, "module must handle %s", eventType)
	require.NoError(t, h(context.Background(), liveCtx(eventType, ts), func(*module.Output) {}))
}

func liveTestDeps(live engine.LiveStore, log *lifecycleLog) engine.Deps {
	return engine.Deps{
		Live:   live,
		Greet:  orderedGreets{log: log},
		Timers: orderedTimers{log: log},
		Seq:    engine.NewSequencer(),
		Log:    zap.NewNop(),
	}
}

// greetsAndArms filters the log to the session-start side effects.
func greetsAndArms(items []string) []string {
	var out []string
	for _, it := range items {
		if it == "greets" || it == "arm" {
			out = append(out, it)
		}
	}
	return out
}

// liveFixture bundles the module under test with the fakes its assertions read.
type liveFixture struct {
	m    module.Module
	live *versionedLive
	log  *lifecycleLog
}

func newLiveFixture() liveFixture {
	fx := liveFixture{log: &lifecycleLog{}}
	fx.live = &versionedLive{log: fx.log}
	fx.m = Live(liveTestDeps(fx.live, fx.log))
	return fx
}

// TestRapidOnlineThenOfflineLeavesChannelOffline drives the exact reported
// symptom: offline right on the heels of online. Effects must land in event
// order and end offline with timers down.
func TestRapidOnlineThenOfflineLeavesChannelOffline(t *testing.T) {
	const on = "2026-08-23T12:00:00Z"
	const off = "2026-08-23T12:00:01Z"

	fx := newLiveFixture()

	runLifecycle(t, fx.m, "stream.online", on)
	runLifecycle(t, fx.m, "stream.offline", off)

	got := waitForLog(t, fx.log, 5)
	assert.Equal(t, []string{
		"set:" + millis(on),
		"greets",
		"arm",
		"clear:" + millis(off),
		"disarm",
	}, got, "offline effects must follow the online's, never interleave")
	assert.False(t, fx.live.isLive, "channel must end offline")
}

// TestStaleOnlineLosingToNewerOfflineIsIgnored covers redelivery skew: an
// older stream.online arriving after a newer offline already won. The store
// rejects the SET, and the module must skip the session-start effects too —
// resetting greets or arming timers would resurrect state the offline just
// cleared.
func TestStaleOnlineLosingToNewerOfflineIsIgnored(t *testing.T) {
	fx := newLiveFixture()

	// The genuine offline wins first.
	runLifecycle(t, fx.m, "stream.offline", "1970-01-01T00:00:02Z")
	waitForLog(t, fx.log, 2) // clear + disarm
	fx.log.reset()
	fx.live.mu.Lock()
	fx.live.setCalls, fx.live.clearCalls = nil, nil
	fx.live.mu.Unlock()

	// An older online redelivered afterwards must change nothing at all.
	runLifecycle(t, fx.m, "stream.online", "1970-01-01T00:00:01Z")

	// The store call itself proves the sequenced task ran to completion.
	assert.Eventually(t, func() bool {
		fx.live.mu.Lock()
		defer fx.live.mu.Unlock()
		return len(fx.live.setCalls) == 1
	}, time.Second, time.Millisecond, "stale online never reached the store")

	assert.Empty(t, fx.log.snapshot(), "superseded online must apply no state")
	assert.Empty(t, greetsAndArms(fx.log.snapshot()))
	assert.False(t, fx.live.isLive)
	assert.Equal(t, []int64{1000}, fx.live.setCalls, "the stale SET must have reached the store and lost")
}

// TestNewerOnlineAfterOfflineStartsCleanSession is the mirror image: a genuine
// online arriving after an offline must fully start the session (greets,
// timers armed, live).
func TestNewerOnlineAfterOfflineStartsCleanSession(t *testing.T) {
	fx := newLiveFixture()

	runLifecycle(t, fx.m, "stream.offline", "1970-01-01T00:00:01Z")
	runLifecycle(t, fx.m, "stream.online", "1970-01-01T00:00:05Z")

	got := waitForLog(t, fx.log, 5)
	assert.Equal(t, []string{"clear:1000", "disarm", "set:5000", "greets", "arm"}, got)
	assert.True(t, fx.live.isLive)
}
