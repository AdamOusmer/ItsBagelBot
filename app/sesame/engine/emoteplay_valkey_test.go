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
func TestEmotePlayScriptIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := newHotPathTestClient(t)
	store := NewValkeyEmotePlay(client)

	// step feeds one candidate line to a fresh-namespaced store and requires no
	// error; each subtest builds its own channel id so state never collides.
	channel := 0
	step := func(t *testing.T, msgID, emote string, width, copies int) EmotePlayResult {
		t.Helper()
		res, err := store.Bump(ctx, EmotePlayUpdate{
			BroadcasterID: uint64(channel), MsgID: msgID, Emote: emote, Width: width, Copies: copies,
		})
		require.NoError(t, err)
		return res
	}
	nextChannel := func() { channel++ }

	t.Run("full pyramid completes once at the base", func(t *testing.T) {
		nextChannel()
		for _, w := range []int{1, 2} {
			require.False(t, step(t, "m"+strconv.Itoa(w), "Kappa", w, 1).PyramidDone)
		}
		r3 := step(t, "m3", "Kappa", 3, 1)
		require.False(t, r3.PyramidDone)
		require.Equal(t, 0, r3.Apex, "apex is only reported on completion")
		require.False(t, step(t, "m2d", "Kappa", 2, 1).PyramidDone)
		done := step(t, "m1d", "Kappa", 1, 1)
		require.True(t, done.PyramidDone)
		require.Equal(t, 3, done.Apex)
		// After completion the state is cleared: repeating the base line starts
		// a fresh attempt, it does not complete again.
		require.False(t, step(t, "after", "Kappa", 1, 1).PyramidDone)
	})

	t.Run("same-width duplicates never double-step", func(t *testing.T) {
		nextChannel()
		step(t, "a", "Kappa", 1, 1)
		step(t, "b", "Kappa", 2, 1)
		// Two chatters racing the same step (or two pods delivering one line
		// near-simultaneously): the second must be neutral.
		step(t, "c", "Kappa", 2, 1)
		step(t, "d", "Kappa", 3, 1)
		require.False(t, step(t, "e", "Kappa", 3, 1).PyramidDone)
		require.False(t, step(t, "f", "Kappa", 4, 1).PyramidDone)
	})

	t.Run("foreign emote restarts, apex turn still descends", func(t *testing.T) {
		nextChannel()
		step(t, "a", "Kappa", 1, 1)
		step(t, "b", "Kappa", 2, 1)
		// Different emote mid-pyramid: the old attempt is abandoned and the new
		// attempt anchors at this line (width 2).
		step(t, "c", "PogChamp", 2, 1)
		done := step(t, "d", "PogChamp", 1, 1)
		require.True(t, done.PyramidDone, "a fresh attempt may descend straight off its anchor")
		require.Equal(t, 2, done.Apex)
	})

	t.Run("width jump restarts instead of completing", func(t *testing.T) {
		nextChannel()
		step(t, "a", "Kappa", 1, 1)
		step(t, "b", "Kappa", 2, 1)
		step(t, "c", "Kappa", 5, 1) // jump: attempt restarts anchored at 5
		// Descending from that anchor would complete a 5-tall pyramid nobody
		// built — but it IS the anchored apex, so this is legal by the rules;
		// what matters is that the jump did not preserve the old ascent.
		done := step(t, "d", "Kappa", 4, 1)
		require.False(t, done.PyramidDone)
	})

	t.Run("replayed message id applies nothing", func(t *testing.T) {
		nextChannel()
		first := step(t, "same", "Kappa", 1, 1)
		require.False(t, first.StreakMilestone)
		for i := 0; i < 3; i++ {
			replay := step(t, "same", "Kappa", 1, 1)
			require.False(t, replay.StreakMilestone, "redelivery must not recount")
			require.False(t, replay.PyramidDone)
		}
		next := step(t, "other", "Kappa", 1, 1)
		require.False(t, next.StreakMilestone, "count is 2, under the first rung")
	})

	t.Run("streak ladder crossing with folded cohorts", func(t *testing.T) {
		nextChannel()
		step(t, "s1", "Kappa", 1, streakLadder[0]-1) // one below the first rung
		crossed := step(t, "s2", "Kappa", 1, 2)
		require.True(t, crossed.StreakMilestone, "the cohort steps over the rung")
		require.Equal(t, streakLadder[0], crossed.Streak, "the announced value is the rung, not the raw count")
		silent := step(t, "s3", "Kappa", 1, 1)
		require.False(t, silent.StreakMilestone, "between rungs")
		switched := step(t, "s4", "PogChamp", 1, 1)
		require.False(t, switched.StreakMilestone, "a new emote restarts from 1")
	})

	t.Run("wide line breaks the streak silently", func(t *testing.T) {
		nextChannel()
		step(t, "w1", "Kappa", 1, streakLadder[0]-1)
		step(t, "w2", "Kappa", 3, 1) // someone starts building: the streak resets
		wide := step(t, "w3", "Kappa", 1, 1)
		require.False(t, wide.StreakMilestone)
	})

	t.Run("expired window restarts both chains", func(t *testing.T) {
		short := &ValkeyEmotePlay{client: client, pyrWin: 40 * time.Millisecond, stkWin: 40 * time.Millisecond}
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
	})

	t.Run("concurrent replicas complete the pyramid exactly once", func(t *testing.T) {
		const replicas = 16
		racing := &ValkeyEmotePlay{client: client, pyrWin: time.Minute, stkWin: time.Minute}
		// Build to descending-through-2, then race every replica at the final
		// width-1 line with distinct message ids — the shape of two pods plus a
		// redelivery all seeing the same chat moment.
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
	})
}
