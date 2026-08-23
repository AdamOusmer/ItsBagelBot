// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// These tests are opt-in because the pyramid and streak transitions are a Lua
// state machine whose semantics only exist inside a real Valkey interpreter.
// They use the same VALKEY_TEST_ADDR convention as valkey_hotpath_test.go. The
// script's windows are plain arguments, so expiry is exercised with millisecond
// windows rather than sleeping out the production values.

// emoteplayFixture hands each test a real client and its own channel id space,
// so state never collides across tests.
type emoteplayFixture struct {
	t     *testing.T
	ctx   context.Context
	store *ValkeyEmotePlay
	seq   int
}

func newEmotePlayFixture(t *testing.T) *emoteplayFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return &emoteplayFixture{t: t, ctx: ctx, store: NewValkeyEmotePlay(newHotPathTestClient(t))}
}

// channel returns a fresh broadcaster id per call; the key namespace is the id.
func (f *emoteplayFixture) channel() int {
	f.seq++
	return f.seq
}

func (f *emoteplayFixture) bump(channel int, msgID string, width, copies int) EmotePlayResult {
	f.t.Helper()
	return f.storeBumpEmote(channel, msgID, "Kappa", width, copies)
}

// storeBumpEmote is bump with the emote spelled out; most tests only ever use
// one token, so bump keeps their bodies quiet.
func (f *emoteplayFixture) storeBumpEmote(channel int, msgID, emote string, width, copies int) EmotePlayResult {
	f.t.Helper()
	res, err := f.store.Bump(f.ctx, EmotePlayUpdate{
		BroadcasterID: uint64(channel), MsgID: msgID, Emote: emote, Width: width, Copies: copies,
	})
	require.NoError(f.t, err)
	return res
}

func TestEmotePlayPyramidCompletesOnceAtTheBase(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	for _, w := range []int{1, 2} {
		require.False(t, f.bump(ch, "m"+strconv.Itoa(w), w, 1).PyramidDone)
	}
	r3 := f.bump(ch, "m3", 3, 1)
	require.False(t, r3.PyramidDone)
	require.Equal(t, 0, r3.Apex, "apex is only reported on completion")
	require.False(t, f.bump(ch, "m2d", 2, 1).PyramidDone)
	done := f.bump(ch, "m1d", 1, 1)
	require.True(t, done.PyramidDone)
	require.Equal(t, 3, done.Apex)
	// After completion the state is cleared: repeating the base line starts a
	// fresh attempt, it does not complete again.
	require.False(t, f.bump(ch, "after", 1, 1).PyramidDone)
}

func TestEmotePlaySameWidthDuplicatesNeverDoubleStep(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(ch, "a", 1, 1)
	f.bump(ch, "b", 2, 1)
	// Two chatters racing the same step (or two pods delivering one line
	// near-simultaneously): the second must be neutral.
	f.bump(ch, "c", 2, 1)
	f.bump(ch, "d", 3, 1)
	require.False(t, f.bump(ch, "e", 3, 1).PyramidDone)
	require.False(t, f.bump(ch, "f", 4, 1).PyramidDone)
}

func TestEmotePlayForeignEmoteRestartsAndApexTurnStillDescends(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(ch, "a", 1, 1)
	f.bump(ch, "b", 2, 1)
	// Different emote mid-pyramid: the old attempt is abandoned and the new
	// attempt anchors at this line (width 2).
	f.bump(ch, "c", 2, 1)
	done := f.bump(ch, "d", 1, 1)
	require.True(t, done.PyramidDone, "a fresh attempt may descend straight off its anchor")
	require.Equal(t, 2, done.Apex)
}

func TestEmotePlayWidthJumpRestartsInsteadOfCompleting(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(ch, "a", 1, 1)
	f.bump(ch, "b", 2, 1)
	f.bump(ch, "c", 5, 1) // jump: attempt restarts anchored at 5
	// Descending from that anchor is legal by the apex-turn rule; what matters
	// is that the jump did not preserve the old ascent as a taller pyramid.
	require.False(t, f.bump(ch, "d", 4, 1).PyramidDone)
}

func TestEmotePlayReplayedMessageIDAppliesNothing(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	first := f.bump(ch, "same", 1, 1)
	require.False(t, first.StreakMilestone)
	for i := 0; i < 3; i++ {
		replay := f.bump(ch, "same", 1, 1)
		require.False(t, replay.StreakMilestone, "redelivery must not recount")
		require.False(t, replay.PyramidDone)
	}
	next := f.bump(ch, "other", 1, 1)
	require.False(t, next.StreakMilestone, "count is 2, under the first rung")
}

func TestEmotePlayStreakLadderCrossingWithFoldedCohorts(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(ch, "s1", 1, streakLadder[0]-1) // one below the first rung
	crossed := f.bump(ch, "s2", 1, 2)
	require.True(t, crossed.StreakMilestone, "the cohort steps over the rung")
	require.Equal(t, streakLadder[0], crossed.Streak, "the announced value is the rung, not the raw count")
	silent := f.bump(ch, "s3", 1, 1)
	require.False(t, silent.StreakMilestone, "between rungs")
	switched := f.storeBumpEmote(ch, "s4", "PogChamp", 1, 1)
	require.False(t, switched.StreakMilestone, "a new emote restarts from 1")
}

func TestEmotePlayWideLineBreaksTheStreakSilently(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(ch, "w1", 1, streakLadder[0]-1)
	f.bump(ch, "w2", 3, 1) // someone starts building: the streak resets
	wide := f.storeBumpEmote(ch, "w3", "Kappa", 1, 1)
	require.False(t, wide.StreakMilestone)
}

func TestEmotePlayExpiredWindowRestartsBothChains(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	short := &ValkeyEmotePlay{client: newHotPathTestClient(t), pyrWin: 40 * time.Millisecond, stkWin: 40 * time.Millisecond}
	bumpShort := func(msgID string, width int) EmotePlayResult {
		res, err := short.Bump(ctx, EmotePlayUpdate{MsgID: msgID, Emote: "Kappa", Width: width, Copies: 1})
		require.NoError(t, err)
		return res
	}
	require.False(t, bumpShort("t1", 1).PyramidDone)
	require.False(t, bumpShort("t2", 2).PyramidDone)
	time.Sleep(60 * time.Millisecond) // one window + slack, well inside any redelivery scale
	require.False(t, bumpShort("t3", 1).StreakMilestone, "the streak expired with the window")
	require.False(t, bumpShort("t4", 2).PyramidDone,
		"the pyramid expired: width 2 starts a fresh attempt instead of ascending the dead one")
}

func TestEmotePlayConcurrentReplicasCompleteExactlyOnce(t *testing.T) {
	const replicas = 16
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	// Build to descending-through-2, then race every replica at the final
	// width-1 line with distinct message ids — the shape of two pods plus a
	// redelivery all seeing the same chat moment.
	racing := &ValkeyEmotePlay{client: newHotPathTestClient(t), pyrWin: time.Minute, stkWin: time.Minute}
	for _, w := range []struct {
		msgID string
		width int
	}{{"p1", 1}, {"p2", 2}, {"p3", 3}, {"p4", 2}} {
		res, err := racing.Bump(ctx, EmotePlayUpdate{MsgID: w.msgID, Emote: "Kappa", Width: w.width, Copies: 1})
		require.NoError(t, err)
		require.False(t, res.PyramidDone)
	}
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		completions int
	)
	wg.Add(replicas)
	for i := 0; i < replicas; i++ {
		go func(i int) {
			defer wg.Done()
			res, err := racing.Bump(ctx, EmotePlayUpdate{
				MsgID: "race-" + strconv.Itoa(i), Emote: "Kappa", Width: 1, Copies: 1,
			})
			require.NoError(t, err)
			if res.PyramidDone {
				mu.Lock()
				completions++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, completions,
		"exactly one replica may observe the completion; every other racer must linearize behind it")
}
