// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

type fakeResetter struct {
	calls atomic.Int64
	fail  atomic.Bool
}

func (f *fakeResetter) BalanceReset(context.Context, uint64) error {
	f.calls.Add(1)
	if f.fail.Load() {
		return errors.New("db down")
	}
	return nil
}

type resetFixture struct {
	clock    *LoyaltyResetClock
	resetter *fakeResetter
	client   valkey.Client
	id       uint64
	now      time.Time
}

var resetNow = time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)

func newResetFixture(t *testing.T, schedule string) *resetFixture {
	t.Helper()
	client := newHotPathTestClient(t)
	id := freshChannel()
	view := projection.ModuleView{Name: LoyaltyModuleName, IsEnabled: true, Configs: codec.RawMessage(`{"resetSchedule":"` + schedule + `"}`)}
	reader := fakeReader{modules: map[string]projection.ModuleView{LoyaltyModuleName: view}}
	f := &resetFixture{resetter: &fakeResetter{}, client: client, id: id, now: resetNow}
	f.clock = NewLoyaltyResetClock(client, reader, f.resetter, LoyaltyResetConfig{})
	f.clock.now = func() time.Time { return resetNow }
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(f.key(), f.claimKey()).Build())
		client.Do(context.Background(), client.B().Zrem().Key(loyaltyResetDueKey).Member(strconv.FormatUint(id, 10)).Build())
	})
	return f
}

func (f *resetFixture) key() string      { return cache.UserKey(loyaltyResetKeyPrefix, f.id) }
func (f *resetFixture) claimKey() string { return cache.UserKey(loyaltyResetClaimPrefix, f.id) }

func (f *resetFixture) dueScore(t *testing.T) (int64, bool) {
	t.Helper()
	score, err := f.client.Do(t.Context(), f.client.B().Zscore().Key(loyaltyResetDueKey).Member(strconv.FormatUint(f.id, 10)).Build()).AsFloat64()
	if valkey.IsValkeyNil(err) {
		return 0, false
	}
	require.NoError(t, err)
	return int64(score), true
}

func (f *resetFixture) releaseClaim(t *testing.T) {
	t.Helper()
	require.NoError(t, f.client.Do(t.Context(), f.client.B().Del().Key(f.claimKey()).Build()).Error())
}

func TestResetFireResetsOncePerPeriodAndRearms(t *testing.T) {
	f := newResetFixture(t, "weekly")
	ctx := t.Context()

	f.clock.Fire(ctx, f.id)
	f.clock.Fire(ctx, f.id)
	assert.EqualValues(t, 1, f.resetter.calls.Load(), "a held claim blocks a doubled trigger")

	f.releaseClaim(t)
	f.clock.Fire(ctx, f.id)
	assert.EqualValues(t, 1, f.resetter.calls.Load(), "the period marker blocks a second reset")

	next := NextResetBoundary(ResetWeekly, resetNow, time.UTC).UnixMilli()
	score, ok := f.dueScore(t)
	assert.True(t, ok)
	assert.Equal(t, next, score)
	ttl, err := f.client.Do(ctx, f.client.B().Pttl().Key(f.key()).Build()).AsInt64()
	require.NoError(t, err)
	assert.Positive(t, ttl)
}

func TestResetFireNextPeriodResetsAgain(t *testing.T) {
	f := newResetFixture(t, "weekly")
	f.clock.Fire(t.Context(), f.id)
	f.releaseClaim(t)
	f.clock.now = func() time.Time { return resetNow.AddDate(0, 0, 7) }
	f.clock.Fire(t.Context(), f.id)
	assert.EqualValues(t, 2, f.resetter.calls.Load())
}

func TestResetOffNeverResets(t *testing.T) {
	for _, schedule := range []string{"off", "", "daily"} {
		f := newResetFixture(t, schedule)
		require.NoError(t, f.clock.arm(t.Context(), f.id, ResetWeekly))
		f.clock.Fire(t.Context(), f.id)
		assert.Zero(t, f.resetter.calls.Load(), schedule)
		_, armed := f.dueScore(t)
		assert.False(t, armed, "disarmed")
	}
}

func TestResetFailureRetriesWithoutMarkingPeriodDone(t *testing.T) {
	f := newResetFixture(t, "monthly")
	f.resetter.fail.Store(true)
	f.clock.Fire(t.Context(), f.id)
	_, armed := f.dueScore(t)
	assert.False(t, armed, "a failed reset does not advance the clock")

	f.resetter.fail.Store(false)
	f.releaseClaim(t)
	f.clock.Fire(t.Context(), f.id)
	assert.EqualValues(t, 2, f.resetter.calls.Load())
	_, armed = f.dueScore(t)
	assert.True(t, armed)
}

func TestResetSweepFiresOverdueAndConvergeArms(t *testing.T) {
	f := newResetFixture(t, "weekly")
	ctx := t.Context()
	member := strconv.FormatUint(f.id, 10)
	require.NoError(t, f.client.Do(ctx, f.client.B().Zadd().Key(loyaltyResetDueKey).ScoreMember().
		ScoreMember(float64(resetNow.Add(-time.Hour).UnixMilli()), member).Build()).Error())

	f.clock.Sweep(ctx)
	require.Eventually(t, func() bool { return f.resetter.calls.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		score, ok := f.dueScore(t)
		return ok && score > resetNow.UnixMilli()
	}, 3*time.Second, 10*time.Millisecond)

	f.clock.Converge(ctx, f.id)
	assert.EqualValues(t, 1, f.resetter.calls.Load(), "arming alone never resets")
}
