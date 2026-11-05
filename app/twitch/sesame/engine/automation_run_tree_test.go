// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (fx *autoFixture) start(t *testing.T, broadcasterID, name, requestID string) string {
	t.Helper()
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{
		BroadcasterID: broadcasterID, Name: name, RequestID: requestID, Source: automationsrpc.SourceDashboard,
	})
	require.True(t, reply.Accepted, reply.Refusal)
	return reply.RunID
}

func (fx *autoFixture) awaitDetachedDone(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return fx.p.autoExec.running.Load() == 0 }, 15*time.Second, 5*time.Millisecond)
}

func TestRunChildContinuationsShareTheForkCap(t *testing.T) {
	fx := newAutoFixture(
		manual("main", loopOf("20", loopOf("10", runChild("child")))),
		manual("child", delayOf(1500), addVar("late")),
	)
	fx.start(t, "1", "main", "req-child-forks")
	assert.LessOrEqual(t, fx.p.autoExec.running.Load(), int32(maxForksPerRun))
	fx.awaitDetachedDone(t)
	assert.Len(t, fx.vars.writesSoFar(), maxForksPerRun)
}

func TestRunChildFloodStaysUnderTheForkCap(t *testing.T) {
	fx := newAutoFixture(
		manual("main", loopOf("100", loopOf("10", runChild("child")))),
		manual("child", delayOf(1500), chat("late")),
	)
	fx.start(t, "1", "main", "req-child-flood")
	assert.LessOrEqual(t, fx.p.autoExec.running.Load(), int32(maxForksPerRun))
	fx.awaitDetachedDone(t)
}

func TestRunChildLaterDelaysDoNotSpendForks(t *testing.T) {
	steps := make([]automation.Action, 0, 2*maxForksPerRun+2)
	for range maxForksPerRun + 1 {
		steps = append(steps, delayOf(1), addVar("step"))
	}
	fx := newAutoFixture(manual("main", runChild("child")), manual("child", steps...))
	fx.start(t, "1", "main", "req-child-chain")
	require.Eventually(t, func() bool { return len(fx.vars.writesSoFar()) == maxForksPerRun+1 }, 10*time.Second, 5*time.Millisecond)
	fx.awaitDetachedDone(t)
}

func TestOneChannelCannotTakeEveryContinuation(t *testing.T) {
	path := actions(delayOf(1500), addVar("late"))
	fx := newAutoFixture(
		manual("flood", loopOf("100", parallelOf(path, path))),
		manual("other", delayOf(1), addVar("other")),
	)
	for i := range 5 {
		fx.start(t, "1", "flood", "req-flood-"+strconv.Itoa(i))
	}
	assert.LessOrEqual(t, fx.p.autoExec.running.Load(), int32(maxContinuationsPerChannel))

	rec := fx.awaitRecord(t, fx.start(t, "2", "other", "req-other"))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	fx.awaitDetachedDone(t)
	assert.Empty(t, fx.p.autoExec.channels, "an idle channel leaves no counter behind")
}

func TestChatCapSpansRunChildren(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("20", runChild("child"))), manual("child", chat("hi")))
	fx.start(t, "1", "main", "req-chat-children")
	fx.awaitDetachedDone(t)
	assert.Len(t, fx.messages(t), automation.MaxChatMessagesPerRun)
}

func TestChatCapSpansParallelPaths(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("20", parallelOf(actions(delayOf(1), chat("hi"))))))
	fx.start(t, "1", "main", "req-chat-forks")
	fx.awaitDetachedDone(t)
	assert.Len(t, fx.messages(t), automation.MaxChatMessagesPerRun)
}

func TestChatCapSpansRunChildrenAfterADelay(t *testing.T) {
	fx := newAutoFixture(
		manual("main", loopOf("20", runChild("child"))),
		manual("child", delayOf(1), chat("hi")),
	)
	fx.start(t, "1", "main", "req-chat-delayed-children")
	fx.awaitDetachedDone(t)
	assert.Len(t, fx.messages(t), automation.MaxChatMessagesPerRun)
}

func TestActionBudgetSpansParallelPaths(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("20", parallelOf(actions(delayOf(1), loopOf("100", addVar("n")))))))
	fx.start(t, "1", "main", "req-action-forks")
	fx.awaitDetachedDone(t)
	assert.NotEmpty(t, fx.vars.writesSoFar())
	assert.LessOrEqual(t, len(fx.vars.writesSoFar()), automation.MaxActions)
}

func gatedAfterDelay() []automation.Action {
	return []automation.Action{delayMS(1), act("if", obj{
		"condition": obj{"type": "perm", "params": obj{"level": "mod"}},
		"then":      []automation.Action{chat("mod path")},
		"else":      []automation.Action{chat("viewer path")},
	})}
}

func TestPermAfterADelayKeepsTheViewersRole(t *testing.T) {
	for badge, want := range map[string]string{"moderator": "mod path", "": "viewer path"} {
		t.Run("badge "+badge, func(t *testing.T) {
			gated := command("!gated", map[string]any{})
			gated.Definition.Actions = gatedAfterDelay()
			p, pub, runs, _ := continuationRig(gated)
			process(t, p, chatEnv("!gated", badge))
			require.Eventually(t, func() bool { return len(runs.forAutomation(gated.ID)) == 1 }, 3*time.Second, 5*time.Millisecond)
			var texts []string
			for _, c := range pub.snapshot() {
				for _, m := range flattenMessage(t, c.msg) {
					texts = append(texts, chatMessageText(t, m))
				}
			}
			assert.Equal(t, []string{want}, texts)
		})
	}
}

func TestPermAfterADelayKeepsTheRoleOfASlotHolder(t *testing.T) {
	gated := command("!gated", map[string]any{})
	gated.Definition.Queue = queueName
	gated.Definition.Actions = gatedAfterDelay()
	rig := newQueueRig(t, queueRigOptions{lease: queueLongHold, bid: 123}, gated)
	process(t, rig.replicas[0], chatEnv("!gated", "moderator"))
	assert.Equal(t, []string{"mod path"}, rig.awaitTexts(t, 1))
}

func TestPermAfterADelayInARunChildKeepsTheRole(t *testing.T) {
	gated := command("!gated", map[string]any{})
	gated.Definition.Actions = []automation.Action{delayMS(1), runChild("child")}
	child := manual("child", gatedAfterDelay()...)
	p, pub, runs, _ := continuationRig(gated, child)
	process(t, p, chatEnv("!gated", "moderator"))
	require.Eventually(t, func() bool { return len(runs.forAutomation(child.ID)) == 1 }, 3*time.Second, 5*time.Millisecond)
	require.Len(t, pub.snapshot(), 1)
	assert.Equal(t, "mod path", chatMessageText(t, flattenMessage(t, pub.snapshot()[0].msg)[0]))
}

func TestSequentialParallelNodesKeepOwnReplayIDs(t *testing.T) {
	fx := newAutoFixture(manual("main",
		parallelOf(actions(delayOf(1), chat("first"))),
		parallelOf(actions(delayOf(1), chat("second"))),
	))
	fx.start(t, "1", "main", "req-fork-ids")
	fx.awaitDetachedDone(t)
	first := publishedIDs(fx)
	require.Len(t, first, 2)
	assert.NotEmpty(t, first[0])
	assert.NotEqual(t, first[0], first[1], "outgress drops a repeated replay id")

	fx.start(t, "1", "main", "req-fork-ids")
	fx.awaitDetachedDone(t)
	again := publishedIDs(fx)
	require.Len(t, again, 4)
	assert.ElementsMatch(t, first, again[2:], "a redelivered event keeps its replay ids")
}

func TestForksInLoopAndChildrenKeepOwnReplayIDs(t *testing.T) {
	fx := newAutoFixture(
		manual("main",
			loopOf("2", parallelOf(actions(delayOf(1), addVar("a"), chat("loop")))),
			runChild("child"),
		),
		manual("child", parallelOf(actions(delayOf(1), chat("child")))),
	)
	fx.start(t, "1", "main", "req-fork-mix")
	fx.awaitDetachedDone(t)
	ids := publishedIDs(fx)
	require.Len(t, ids, 3)
	slices.Sort(ids)
	assert.Len(t, slices.Compact(ids), 3)
}

func publishedIDs(fx *autoFixture) []string {
	var ids []string
	for _, c := range fx.pub.snapshot() {
		ids = append(ids, c.id)
	}
	return ids
}

func TestRunCalledTwiceInOneIterationAppliesTwice(t *testing.T) {
	fx := newAutoFixture(
		manual("main", runChild("child"), runChild("child")),
		manual("child", addVar("total")),
	)
	fx.run(t, "main", "req-run-twice", nil)
	assert.Equal(t, "2", fx.vars.value(channelVars, "total"))

	fx.run(t, "main", "req-run-twice", nil)
	assert.Equal(t, "2", fx.vars.value(channelVars, "total"), "a redelivered event repeats neither call")
}

func TestRunCalledTwiceInsideALoopAppliesPerCallPerIteration(t *testing.T) {
	fx := newAutoFixture(
		manual("main", loopOf("3", runChild("child"), runChild("child"))),
		manual("child", delayOf(1), addVar("total")),
	)
	fx.start(t, "1", "main", "req-run-twice-loop")
	fx.awaitDetachedDone(t)
	assert.Equal(t, "6", fx.vars.value(channelVars, "total"))
}

func TestRewardUpdateRecordsItWasQueuedNotConfirmed(t *testing.T) {
	fx := newAutoFixture(manual("main", act("reward_update", obj{"reward_id": "rw-1", "cost": 50})))
	rec := fx.run(t, "main", "req-reward-queued", nil)
	assert.Equal(t, []string{"0:reward_update:ok:queued"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
}
