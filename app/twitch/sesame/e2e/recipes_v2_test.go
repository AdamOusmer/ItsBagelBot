// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package e2e

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	e2eFixture = "recipes_v2_e2e.json"
	e2eID      = "recipes_v2_e2e"
	rewardID   = "hydrate"
	loopDelay  = time.Second
	settle     = 15 * time.Second
)

func uniqueChannel(offset uint64) uint64 {
	return uint64(time.Now().UnixMilli())*10 + offset
}

func (s *stack) awaitChat(t *testing.T, broadcaster uint64, count int) []chatLine {
	t.Helper()
	var lines []chatLine
	reached := assert.Eventually(t, func() bool {
		lines = s.helix.chatLines(t, broadcaster)
		return len(lines) >= count
	}, settle, 20*time.Millisecond, "wanted %d chat lines on %d", count, broadcaster)
	if !reached {
		s.logRuns(t, broadcaster)
		t.Fatalf("chat so far: %q", texts(lines))
	}
	return lines
}

func (s *stack) logRuns(t *testing.T, broadcaster uint64) {
	t.Helper()
	runs, err := s.runs.Recent(context.Background(), broadcaster, e2eID, 10)
	t.Logf("run log err=%v count=%d", err, len(runs))
	for _, run := range runs {
		t.Logf("run %s outcome=%s reason=%s steps=%v", run.RunID, run.Outcome, run.Reason, stepStatuses(run))
	}
	for _, call := range s.helix.snapshot() {
		t.Logf("helix %s %s %s", call.Method, call.Path, call.Body)
	}
}

func texts(lines []chatLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}

func (s *stack) channelVariable(t *testing.T, broadcaster uint64, name string) string {
	t.Helper()
	got, err := s.vars.VariablesGet(context.Background(), engine.VariableOwner{BroadcasterID: broadcaster, Scope: automation.VariableScopeChannel}, []string{name})
	require.NoError(t, err)
	return got[name]
}

func (s *stack) recentRuns(t *testing.T, broadcaster uint64, count int) []automationrpc.RunRecord {
	t.Helper()
	var runs []automationrpc.RunRecord
	require.Eventually(t, func() bool {
		var err error
		runs, err = s.runs.Recent(context.Background(), broadcaster, e2eID, 10)
		return err == nil && len(runs) >= count
	}, settle, 20*time.Millisecond)
	return runs
}

func (s *stack) awaitRewardStep(t *testing.T, broadcaster uint64, want string) automationrpc.RunRecord {
	t.Helper()
	var rec automationrpc.RunRecord
	require.Eventually(t, func() bool {
		runs, err := s.runs.Recent(context.Background(), broadcaster, e2eID, 10)
		if err != nil || len(runs) == 0 {
			return false
		}
		rec = runs[0]
		return slices.Contains(stepStatuses(rec), want)
	}, settle, 20*time.Millisecond, "wanted step %s", want)
	return rec
}

func stepStatuses(rec automationrpc.RunRecord) []string {
	out := make([]string, len(rec.Actions))
	for i, a := range rec.Actions {
		out[i] = a.Type + ":" + a.Status
		if a.Detail != "" {
			out[i] += ":" + a.Detail
		}
	}
	return out
}

func patchKeys(t *testing.T, body string) map[string]any {
	t.Helper()
	var fields map[string]any
	require.NoError(t, codec.UnmarshalFromString(body, &fields))
	return fields
}

func snapshotExists(t *testing.T, s *stack, broadcaster uint64) bool {
	t.Helper()
	pending, err := engine.NewValkeyRewardResets(s.vc).RestorePending(context.Background(), broadcaster, time.Now().UnixNano())
	require.NoError(t, err)
	return pending
}

// A paid channel's recipe fetch should spend the paid budget, as command fetches do (engine/urlfetch.go).
func TestRecipesV2FetchCarriesThePaidFlag(t *testing.T) {
	channel := uniqueChannel(5)
	s := startStack(t, channel)
	s.helix.seedReward(rewardRef{channel, rewardID}, rewardState{Enabled: true, Cost: 100})
	s.source.set(channel, loadAutomation(t, e2eFixture, channel))

	requests := make(chan gossiprpc.Request, 8)
	sub, err := s.nc.Subscribe(gossiprpc.Subject(gossipPrefix, "custom", "fetch"), func(msg *nats.Msg) {
		var req gossiprpc.Request
		if codec.Unmarshal(msg.Data, &req) == nil {
			requests <- req
		}
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()
	require.NoError(t, s.nc.Flush())

	s.command(t, chatCommand{Broadcaster: channel, Viewer: "alice", Text: "!e2e paid"})
	select {
	case req := <-requests:
		assert.True(t, req.IsPremium, "the event came through the premium lane")
	case <-time.After(settle):
		t.Fatal("no fetch reached gossip")
	}
}

func TestRecipesV2RewardResultReachesRunHistory(t *testing.T) {
	foreign := uniqueChannel(4)
	s := startStack(t, foreign)
	s.helix.seedForeignReward(rewardRef{foreign, rewardID})
	s.source.set(foreign, loadAutomation(t, e2eFixture, foreign))

	s.command(t, chatCommand{Broadcaster: foreign, Viewer: "alice", Text: "!e2e refused"})

	rec := s.awaitRewardStep(t, foreign, "reward_update:failed:reward_not_manageable")
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
}

func assertFirstRunUpstreamAndVariables(t *testing.T, s *stack, channel uint64) {
	t.Helper()
	hits := s.upstream.snapshot()
	require.GreaterOrEqual(t, len(hits), 2)
	assert.Equal(t, []string{"/item/1", "/item/2"}, []string{hits[0].Path, hits[1].Path})

	assert.Equal(t, "{arg:1}", s.channelVariable(t, channel, "last_input"), "viewer text is stored as written")
	assert.Equal(t, "1", s.channelVariable(t, channel, "seen"))
}

func assertFirstRunReward(t *testing.T, s *stack, channel uint64) {
	t.Helper()
	patches := s.helix.patches(channel)
	require.Len(t, patches, 1)
	assert.Equal(t, map[string]any{"is_enabled": false, "cost": float64(500)}, patchKeys(t, patches[0].Body))
	assert.Equal(t, rewardID, patches[0].RewardID)
	assert.Equal(t, rewardState{Enabled: false, Cost: 500}, s.helix.reward(rewardRef{channel, rewardID}))
	assert.True(t, snapshotExists(t, s, channel), "the first change leaves a snapshot")
}

func assertFirstRunRecord(t *testing.T, s *stack, channel uint64) {
	t.Helper()
	rec := s.awaitRewardStep(t, channel, "reward_update:ok")
	t.Logf("run record: %s %v", rec.Outcome, stepStatuses(rec))
	assert.Contains(t, stepStatuses(rec), "http_get:ok")
	assert.Contains(t, stepStatuses(rec), "http_get:failed:upstream_error")
	assert.NotEqual(t, automationrpc.OutcomeFailed, rec.Outcome, "a failure with a handled path is not a failed run")
}

func TestRecipesV2DefinitionToEffects(t *testing.T) {
	single, queued, legacy := uniqueChannel(1), uniqueChannel(2), uniqueChannel(3)
	s := startStack(t, single, queued, legacy)

	s.helix.seedReward(rewardRef{single, rewardID}, rewardState{Enabled: true, Cost: 100})
	s.helix.seedReward(rewardRef{queued, rewardID}, rewardState{Enabled: true, Cost: 100})
	s.source.set(single, loadAutomation(t, e2eFixture, single))
	s.source.set(queued, loadAutomation(t, e2eFixture, queued))
	s.source.set(legacy, loadAutomation(t, "command_defaults.json", legacy))

	t.Run("first run reaches every boundary", func(t *testing.T) {
		started := time.Now()
		s.command(t, chatCommand{Broadcaster: single, Viewer: "alice", Text: "!e2e {arg:1}"})
		lines := s.awaitChat(t, single, 3)

		assert.Equal(t, []string{"worked 1 from-upstream", "failed 2 upstream_error", "done {arg:1} 1"}, texts(lines)[:3])
		assert.GreaterOrEqual(t, lines[1].At.Sub(lines[0].At), loopDelay-100*time.Millisecond, "the loop delay separates iterations")
		assert.GreaterOrEqual(t, lines[2].At.Sub(lines[1].At), loopDelay-100*time.Millisecond, "the last iteration's delay runs before the steps after the loop")
		assert.GreaterOrEqual(t, lines[2].At.Sub(started), 2*loopDelay-100*time.Millisecond)

		assertFirstRunUpstreamAndVariables(t, s, single)
		assertFirstRunReward(t, s, single)
		assertFirstRunRecord(t, s, single)
	})

	t.Run("second run reads the persisted variable and keeps the first snapshot", func(t *testing.T) {
		s.command(t, chatCommand{Broadcaster: single, Viewer: "alice", Text: "!e2e second"})
		lines := s.awaitChat(t, single, 6)
		assert.Equal(t, []string{"worked 1 from-upstream", "failed 2 upstream_error", "done second 2"}, texts(lines)[3:6])
		assert.Equal(t, "2", s.channelVariable(t, single, "seen"))
		require.Eventually(t, func() bool { return len(s.helix.patches(single)) == 2 }, settle, 20*time.Millisecond)
	})

	t.Run("queue serializes concurrent runs across two pipelines", func(t *testing.T) {
		done := make(chan struct{}, 2)
		for replica, arg := range []string{"from-a", "from-b"} {
			go func() {
				defer func() { done <- struct{}{} }()
				s.command(t, chatCommand{Replica: replica, Broadcaster: queued, Viewer: "viewer" + strconv.Itoa(replica), Text: "!e2e " + arg})
			}()
		}
		<-done
		<-done
		lines := s.awaitChat(t, queued, 6)
		got := texts(lines)
		require.Len(t, got, 6, "%v", got)
		t.Logf("queued chat order: %v", got)
		for i := range 2 {
			run := got[i*3 : i*3+3]
			assert.Equal(t, "worked 1 from-upstream", run[0])
			assert.Equal(t, "failed 2 upstream_error", run[1])
			assert.True(t, strings.HasPrefix(run[2], "done from-"), run[2])
			assert.True(t, strings.HasSuffix(run[2], " "+strconv.Itoa(i+1)), "run %d saw the counter from the run before it: %s", i+1, run[2])
		}
		assert.True(t, lines[3].At.After(lines[2].At), "the second run starts after the first ends")
		assert.Equal(t, "2", s.channelVariable(t, queued, "seen"))
	})

	t.Run("stream offline restores and clears through the live module", func(t *testing.T) {
		before := len(s.helix.patches(single))
		s.streamOffline(t, single)
		require.Eventually(t, func() bool { return len(s.helix.patches(single)) == before+1 }, settle, 20*time.Millisecond)
		patch := s.helix.patches(single)[before]
		assert.Equal(t, map[string]any{"is_enabled": true, "cost": float64(100)}, patchKeys(t, patch.Body))
		assert.Equal(t, rewardState{Enabled: true, Cost: 100}, s.helix.reward(rewardRef{single, rewardID}))
		require.Eventually(t, func() bool { return !snapshotExists(t, s, single) }, settle, 20*time.Millisecond, "the snapshot clears")

		s.streamOffline(t, single)
		time.Sleep(2 * time.Second)
		assert.Len(t, s.helix.patches(single), before+1, "a second offline changes nothing")
	})

	t.Run("a definition from before v2 validates and runs the same", func(t *testing.T) {
		s.command(t, chatCommand{Broadcaster: legacy, Viewer: "bob", Text: "!hi"})
		lines := s.awaitChat(t, legacy, 1)
		assert.Equal(t, []string{"hello bob"}, texts(lines))
		assert.Empty(t, s.helix.patches(legacy))
	})
}
