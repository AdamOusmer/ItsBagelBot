// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"math"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	loopedChats        = 50
	plainChatBurst     = 20.0
	plainChatPerSecond = 20.0 / 30.0
	chatSettleQuiet    = 7 * time.Second
	floodCommands      = 1000
	floodPace          = 10 * time.Millisecond
	lightsLoopTimes    = 12
	lightsRefused      = "too many light changes, slow down"
	goveeCallsPerColor = 2
)

type step = map[string]any

func recipeWith(t *testing.T, broadcaster uint64, command string, actions ...step) automation.Automation {
	t.Helper()
	raw, err := codec.Marshal(step{
		"version": 1,
		"trigger": step{"type": "command", "params": step{"name": command}},
		"actions": actions,
	})
	require.NoError(t, err)
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(raw, &def))
	require.Empty(t, automation.Validate(def))
	return automation.Automation{ID: command, UserID: broadcaster, Name: command, Enabled: true, Definition: def}
}

func loopStep(times int, body ...step) step {
	return step{"type": "loop", "params": step{"times": strconv.Itoa(times), "actions": body}}
}

func chatStep(text string) step {
	return step{"type": "chat", "params": step{"text": text}}
}

func (s *stack) settledChat(t *testing.T, broadcaster uint64) []chatLine {
	t.Helper()
	var lines []chatLine
	changed := time.Now()
	for time.Since(changed) < chatSettleQuiet {
		time.Sleep(100 * time.Millisecond)
		if got := s.helix.chatLines(t, broadcaster); len(got) != len(lines) {
			lines, changed = got, time.Now()
		}
	}
	return lines
}

func assertWithinPlainChatBucket(t *testing.T, lines []chatLine) {
	t.Helper()
	require.NotEmpty(t, lines)
	elapsed := lines[len(lines)-1].At.Sub(lines[0].At).Seconds()
	allowed := plainChatBurst + math.Ceil(plainChatPerSecond*elapsed) + 1
	assert.LessOrEqual(t, float64(len(lines)), allowed, "outgress's bucket holds the channel to its Twitch budget")
}

func TestRecipesDownstreamOwnsTheBuckets(t *testing.T) {
	modChannel, batched, paced, lights, flooded := uniqueChannel(11), uniqueChannel(12), uniqueChannel(13), uniqueChannel(14), uniqueChannel(15)
	s := startStackFor(t, stackChannels{Mod: []uint64{modChannel, lights}, NonMod: []uint64{batched, paced, flooded}})
	lineLoop := loopStep(loopedChats, chatStep("line {local:index}"))

	t.Run("a moderator bot gets every looped chat", func(t *testing.T) {
		s.source.set(modChannel, recipeWith(t, modChannel, "!loop", lineLoop))
		s.command(t, chatCommand{Broadcaster: modChannel, Viewer: "alice", Text: "!loop"})
		got := texts(s.awaitChat(t, modChannel, loopedChats))
		assert.Len(t, got, loopedChats)
		slices.Sort(got)
		assert.Len(t, slices.Compact(got), loopedChats, "no line sent twice")
	})

	t.Run("a plain bot's looped chats stop at outgress's bucket", func(t *testing.T) {
		s.source.set(batched, recipeWith(t, batched, "!loop", lineLoop))
		s.command(t, chatCommand{Broadcaster: batched, Viewer: "alice", Text: "!loop"})
		lines := s.settledChat(t, batched)
		t.Logf("batched: delivered %d of %d, dropped %d: %v", len(lines), loopedChats, loopedChats-len(lines), texts(lines))
		assertWithinPlainChatBucket(t, lines)
	})

	t.Run("a plain bot's chats sent one at a time stop at outgress's bucket", func(t *testing.T) {
		delayed := loopStep(loopedChats, chatStep("line {local:index}"), step{"type": "delay", "params": step{"ms": 20}})
		s.source.set(paced, recipeWith(t, paced, "!loop", delayed))
		s.command(t, chatCommand{Broadcaster: paced, Viewer: "alice", Text: "!loop"})
		lines := s.settledChat(t, paced)
		t.Logf("one at a time: delivered %d of %d, dropped %d", len(lines), loopedChats, loopedChats-len(lines))
		assertWithinPlainChatBucket(t, lines)
	})

	t.Run("gossip refuses lights past its bucket and the step says so", func(t *testing.T) {
		light := step{"type": "lights", "params": step{"device": "AA:BB", "sku": "H6008", "color": "#f5a524"}}
		s.source.set(lights, recipeWith(t, lights, "!lights", loopStep(lightsLoopTimes, light)))
		s.command(t, chatCommand{Broadcaster: lights, Viewer: "alice", Text: "!lights"})
		rec := s.awaitLatestRun(t, lights)
		statuses := stepStatuses(rec)
		t.Logf("lights outcome=%s steps=%v", rec.Outcome, statuses)
		sent := countOf(statuses, "lights:ok")
		assert.Len(t, s.govee.snapshot(), sent*goveeCallsPerColor, "only admitted steps reach Govee")
		assert.Positive(t, sent)
		assert.Equal(t, lightsLoopTimes-sent, countOf(statuses, "lights:failed:"+lightsRefused))
		assert.Positive(t, lightsLoopTimes-sent)
		assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
	})

	t.Run("a flood of chat commands reaches outgress's bucket, not a limit in sesame", func(t *testing.T) {
		s.source.set(flooded, recipeWith(t, flooded, "!flood", chatStep("hi {user}")))
		baseline, peak := runtime.NumGoroutine(), 0
		var longest time.Duration
		for range floodCommands {
			started := time.Now()
			s.command(t, chatCommand{Broadcaster: flooded, Viewer: "alice", Text: "!flood"})
			longest = max(longest, time.Since(started))
			peak = max(peak, runtime.NumGoroutine())
			time.Sleep(floodPace)
		}
		lines := s.settledChat(t, flooded)
		runs, err := s.runs.Recent(context.Background(), flooded, "", automation.RunsKept)
		require.NoError(t, err)
		t.Logf("flood: delivered %d of %d, baseline=%d peak=%d longestProcess=%s runsKept=%d", len(lines), floodCommands, baseline, peak, longest, len(runs))
		assertWithinPlainChatBucket(t, lines)
		assert.Len(t, runs, automation.RunsKept)
		assert.Less(t, longest, 2*time.Second)
	})
}

func (s *stack) awaitLatestRun(t *testing.T, broadcaster uint64) automationrpc.RunRecord {
	t.Helper()
	var rec automationrpc.RunRecord
	require.Eventually(t, func() bool {
		runs, err := s.runs.Recent(context.Background(), broadcaster, "", 1)
		if err != nil || len(runs) == 0 {
			return false
		}
		rec = runs[0]
		return true
	}, settle, 20*time.Millisecond)
	return rec
}

func countOf(values []string, want string) int {
	n := 0
	for _, v := range values {
		if v == want {
			n++
		}
	}
	return n
}
