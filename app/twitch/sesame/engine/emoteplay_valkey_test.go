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

func (f *emoteplayFixture) channel() int {
	f.seq++
	return f.seq
}

type emoteBump struct {
	channel int
	msgID   string
	emote   string
	width   int
	copies  int
}

func (f *emoteplayFixture) bump(b emoteBump) EmotePlayResult {
	f.t.Helper()
	emote := b.emote
	if emote == "" {
		emote = "Kappa"
	}
	res, err := f.store.Bump(f.ctx, EmotePlayUpdate{
		BroadcasterID: uint64(b.channel), MsgID: b.msgID, Emote: emote, Width: b.width, Copies: b.copies,
	})
	require.NoError(f.t, err)
	return res
}

func TestEmotePlayPyramidCompletesOnceAtTheBase(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	for _, w := range []int{1, 2} {
		require.False(t, f.bump(emoteBump{channel: ch, msgID: "m" + strconv.Itoa(w), width: w, copies: 1}).PyramidDone)
	}
	r3 := f.bump(emoteBump{channel: ch, msgID: "m3", width: 3, copies: 1})
	require.False(t, r3.PyramidDone)
	require.Equal(t, 0, r3.Apex, "apex is only reported on completion")
	require.False(t, f.bump(emoteBump{channel: ch, msgID: "m2d", width: 2, copies: 1}).PyramidDone)
	done := f.bump(emoteBump{channel: ch, msgID: "m1d", width: 1, copies: 1})
	require.True(t, done.PyramidDone)
	require.Equal(t, 3, done.Apex)
	require.False(t, f.bump(emoteBump{channel: ch, msgID: "after", width: 1, copies: 1}).PyramidDone)
}

func TestEmotePlaySameWidthDuplicatesNeverDoubleStep(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(emoteBump{channel: ch, msgID: "a", width: 1, copies: 1})
	f.bump(emoteBump{channel: ch, msgID: "b", width: 2, copies: 1})
	f.bump(emoteBump{channel: ch, msgID: "c", width: 2, copies: 1})
	f.bump(emoteBump{channel: ch, msgID: "d", width: 3, copies: 1})
	require.False(t, f.bump(emoteBump{channel: ch, msgID: "e", width: 3, copies: 1}).PyramidDone)
	require.False(t, f.bump(emoteBump{channel: ch, msgID: "f", width: 4, copies: 1}).PyramidDone)
}

func TestEmotePlayRejectsPartialPyramids(t *testing.T) {
	f := newEmotePlayFixture(t)
	for _, tc := range []struct {
		name   string
		widths []int
	}{
		{"descent without an ascent", []int{3, 2, 1}},
		{"missing initial base", []int{2, 3, 2, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := f.channel()
			for i, width := range tc.widths {
				res := f.bump(emoteBump{channel: ch, msgID: tc.name + strconv.Itoa(i), width: width, copies: 1})
				require.False(t, res.PyramidDone)
			}
		})
	}
}

func TestEmotePlayRejectsWidthJumps(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(emoteBump{channel: ch, msgID: "a", width: 1, copies: 1})
	f.bump(emoteBump{channel: ch, msgID: "b", width: 2, copies: 1})
	f.bump(emoteBump{channel: ch, msgID: "c", width: 5, copies: 1})
	for _, width := range []int{4, 3, 2, 1} {
		require.False(t, f.bump(emoteBump{channel: ch, msgID: "after-jump" + strconv.Itoa(width), width: width, copies: 1}).PyramidDone)
	}
}

func TestEmotePlayReplayedMessageIDAppliesNothing(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	first := f.bump(emoteBump{channel: ch, msgID: "same", width: 1, copies: 1})
	require.False(t, first.StreakMilestone)
	for i := 0; i < 3; i++ {
		replay := f.bump(emoteBump{channel: ch, msgID: "same", width: 1, copies: 1})
		require.False(t, replay.StreakMilestone, "redelivery must not recount")
		require.False(t, replay.PyramidDone)
	}
	next := f.bump(emoteBump{channel: ch, msgID: "other", width: 1, copies: 1})
	require.False(t, next.StreakMilestone, "count is 2, under the first rung")
}

func TestEmotePlayStreakLadderCrossingWithFoldedCohorts(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(emoteBump{channel: ch, msgID: "s1", width: 1, copies: streakLadder[0] - 1})
	crossed := f.bump(emoteBump{channel: ch, msgID: "s2", width: 1, copies: 2})
	require.True(t, crossed.StreakMilestone, "the cohort steps over the rung")
	require.Equal(t, streakLadder[0], crossed.Streak, "the announced value is the rung, not the raw count")
	silent := f.bump(emoteBump{channel: ch, msgID: "s3", width: 1, copies: 1})
	require.False(t, silent.StreakMilestone, "between rungs")
	switched := f.bump(emoteBump{channel: ch, msgID: "s4", emote: "PogChamp", width: 1, copies: 1})
	require.False(t, switched.StreakMilestone, "a new emote restarts from 1")
}

func TestEmotePlayWideLineBreaksTheStreakSilently(t *testing.T) {
	f := newEmotePlayFixture(t)
	ch := f.channel()
	f.bump(emoteBump{channel: ch, msgID: "w1", width: 1, copies: streakLadder[0] - 1})
	f.bump(emoteBump{channel: ch, msgID: "w2", width: 3, copies: 1})
	wide := f.bump(emoteBump{channel: ch, msgID: "w3", width: 1, copies: 1})
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
	time.Sleep(60 * time.Millisecond)
	require.False(t, bumpShort("t3", 1).StreakMilestone, "the streak expired with the window")
	require.False(t, bumpShort("t4", 2).PyramidDone,
		"the pyramid expired: width 2 starts a fresh attempt instead of ascending the dead one")
}

func TestEmotePlayConcurrentReplicasCompleteExactlyOnce(t *testing.T) {
	const replicas = 16
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	racing := &ValkeyEmotePlay{client: newHotPathTestClient(t), pyrWin: time.Minute, stkWin: time.Minute}
	for _, w := range []struct {
		msgID string
		width int
	}{{"p1", 1}, {"p2", 2}, {"p3", 3}, {"p4", 2}} {
		res, err := racing.Bump(ctx, EmotePlayUpdate{MsgID: w.msgID, Emote: "Kappa", Width: w.width, Copies: 1})
		require.NoError(t, err)
		require.False(t, res.PyramidDone)
	}
	require.Equal(t, 1, raceWidthOneLines(t, ctx, racing, replicas),
		"exactly one replica may observe the completion; every other racer must linearize behind it")
}

func raceWidthOneLines(t *testing.T, ctx context.Context, store *ValkeyEmotePlay, replicas int) int {
	t.Helper()
	outcomes := make(chan bumpOutcome, replicas)
	var wg sync.WaitGroup
	wg.Add(replicas)
	for i := 0; i < replicas; i++ {
		go func(i int) {
			defer wg.Done()
			res, err := store.Bump(ctx, EmotePlayUpdate{
				MsgID: "race-" + strconv.Itoa(i), Emote: "Kappa", Width: 1, Copies: 1,
			})
			outcomes <- bumpOutcome{res: res, err: err}
		}(i)
	}
	wg.Wait()
	close(outcomes)
	completions := 0
	for out := range outcomes {
		require.NoError(t, out.err)
		if out.res.PyramidDone {
			completions++
		}
	}
	return completions
}

type bumpOutcome struct {
	res EmotePlayResult
	err error
}
