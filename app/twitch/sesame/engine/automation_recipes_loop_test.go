// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoopEffectRunsEveryIterationAndRedeliveryRepeatsNone(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("3", act("points_add", obj{"login": "", "amount": 5}))))
	first := fx.run(t, "main", "req-loop", nil)
	assert.Equal(t, 3, fx.loyalty.adjustCount(), statuses(first))
	assert.Equal(t, automationrpc.OutcomeOK, first.Outcome)
	assert.Equal(t, []string{"0:loop:ok:", "0:points_add:ok:", "0:points_add:ok:", "0:points_add:ok:"}, statuses(first))

	second := fx.run(t, "main", "req-loop", nil)
	assert.Equal(t, 3, fx.loyalty.adjustCount())
	assert.Equal(t, []string{"0:loop:ok:", "0:points_add:skipped:duplicate", "0:points_add:skipped:duplicate", "0:points_add:skipped:duplicate"}, statuses(second))

	fx.run(t, "main", "req-loop-other", nil)
	assert.Equal(t, 6, fx.loyalty.adjustCount())
}

func TestNestedLoopEffectKeysStayDistinctAndStable(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("2", loopOf("3", act("points_add", obj{"login": "", "amount": 1})))))
	fx.run(t, "main", "req-nested", nil)
	assert.Equal(t, 6, fx.loyalty.adjustCount())
	fx.run(t, "main", "req-nested", nil)
	assert.Equal(t, 6, fx.loyalty.adjustCount())
	keys := fx.p.dedup.store.(*recordingStore).keys()
	assert.True(t, slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(k, "auto:main:0.body.0.body.0@2@3") }), "%v", keys)
}

func TestLoopWithDelayResumesEveryIterationOnce(t *testing.T) {
	fx := newAutoFixture(manual("main",
		loopOf("3", delayOf(2), setVar("seen", "{local:index}"), addVar("total")),
		setVar("after", "[{local:index}]"),
	))
	rec := fx.run(t, "main", "req-delay-loop", nil)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, []string{"seen=1", "total+1", "seen=2", "total+1", "seen=3", "total+1", "after=[]"}, fx.vars.writesSoFar())

	fx.run(t, "main", "req-delay-loop", nil)
	assert.Equal(t, 7, len(fx.vars.writesSoFar()), "redelivery repeats nothing")
	assert.Equal(t, "3", fx.vars.value(channelVars, "total"))
}

func TestNestedLoopWithDelayShadowsAndRestoresIndex(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("2",
		act("math", obj{"expression": "{local:index}", "save_as": "outer"}),
		loopOf("2", delayOf(1), setVar("cell", "{local:outer}.{local:index}")),
		setVar("back", "{local:index}"),
	)))
	rec := fx.run(t, "main", "req-nested-delay-loop", nil)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, []string{"cell=1.1", "cell=1.2", "back=1", "cell=2.1", "cell=2.2", "back=2"}, fx.vars.writesSoFar())
}

func TestDelayThatEndsALoopBodyStillResumesOuterSteps(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("2", delayOf(1)), setVar("done", "yes")))
	rec := fx.run(t, "main", "req-trailing-delay", nil)
	assert.Equal(t, []string{"done=yes"}, fx.vars.writesSoFar(), statuses(rec))
}

func TestParallelInsideLoopRunsEachPathPerIteration(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("3",
		parallelOf(actions(delayOf(2), setVar("late", "{local:index}"), act("chat", obj{"text": "late {local:index}"})), actions(setVar("now", "{local:index}"))),
	)))
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: "req-par-loop", Source: automationsrpc.SourceDashboard})
	require.True(t, reply.Accepted)
	writes := fx.awaitWrites(t, 6)
	assert.ElementsMatch(t, []string{"now=1", "now=2", "now=3", "late=1", "late=2", "late=3"}, writes)

	require.Eventually(t, func() bool { return len(fx.pub.snapshot()) == 3 }, 3*time.Second, 2*time.Millisecond)
	ids := map[string]bool{}
	for _, c := range fx.pub.snapshot() {
		ids[c.id] = true
	}
	assert.Len(t, ids, 3, "each path's output needs its own replay id or outgress drops it")
}

func TestLoopInsideParallelPauses(t *testing.T) {
	fx := newAutoFixture(manual("main",
		parallelOf(actions(loopOf("2", delayOf(1), setVar("a", "{local:index}"))), actions(setVar("b", "x"))),
		setVar("after", "ok"),
	))
	fx.run(t, "main", "req-loop-par", nil)
	writes := fx.awaitWrites(t, 4)
	assert.Equal(t, []string{"a=1", "a=2"}, slices.DeleteFunc(writes, func(s string) bool { return !strings.HasPrefix(s, "a=") }))
}

func TestStopInsideLoopEndsTheWholeRun(t *testing.T) {
	cases := map[string][]automation.Action{
		"before any delay": actions(setVar("a", "{local:index}"), act("stop", obj{})),
		"after a delay":    actions(delayOf(1), setVar("a", "{local:index}"), act("stop", obj{})),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", loopOf("5", body...), setVar("after", "x")))
			rec := fx.run(t, "main", "req-stop-loop", nil)
			assert.Equal(t, []string{"a=1"}, fx.vars.writesSoFar())
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
		})
	}
}

func TestStopInsideLoopInParallelEndsOnlyThatPath(t *testing.T) {
	fx := newAutoFixture(manual("main",
		parallelOf(actions(loopOf("5", setVar("a", "{local:index}"), act("stop", obj{}))), actions(setVar("b", "x"))),
		setVar("after", "y"),
	))
	fx.run(t, "main", "req-stop-par", nil)
	assert.Equal(t, []string{"a=1", "b=x", "after=y"}, fx.vars.writesSoFar())
}

func TestLoopTimes(t *testing.T) {
	cases := []struct {
		times string
		count int
		want  string
	}{
		{"3", 3, "0:loop:ok:"},
		{"0", 0, "0:loop:ok:"},
		{"-4", 0, "0:loop:ok:"},
		{"500", automation.MaxLoopTimes, "0:loop:ok:"},
		{"999999999999999999999999", automation.MaxLoopTimes, "0:loop:ok:"},
		{"abc", 0, "0:loop:failed:bad_times"},
		{"2.5", 0, "0:loop:failed:bad_times"},
		{"", 0, "0:loop:failed:bad_times"},
	}
	for _, tc := range cases {
		t.Run(tc.times, func(t *testing.T) {
			fx := newAutoFixture(manual("main", loopOf("{arg:n}", addVar("c"))))
			rec := fx.run(t, "main", "req-times", map[string]string{"n": tc.times})
			assert.Equal(t, tc.want, statuses(rec)[0])
			assert.Len(t, fx.vars.writesSoFar(), tc.count)
		})
	}
}

func TestLoopConsumesNoBudgetButItsBodyDoes(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("100", loopOf("100", addVar("c")))))
	rec := fx.run(t, "main", "req-budget", nil)
	assert.Len(t, fx.vars.writesSoFar(), automation.MaxActions)
	assert.Contains(t, statuses(rec), "0:loop:limited:max_actions")
}

func TestBranchOnlyLoopsCannotSpinForever(t *testing.T) {
	spin := act("if", obj{"condition": obj{"type": "live", "params": obj{"state": "live"}}})
	for range 8 {
		spin = loopOf("100", spin)
	}
	fx := newAutoFixture(manual("main", spin))
	rec := fx.run(t, "main", "req-spin", nil)
	assert.LessOrEqual(t, len(rec.Actions), automation.MaxRecordedActions)
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
}

func TestRunChildrenInsideLoopsShareTheStepBudget(t *testing.T) {
	fx := newAutoFixture(
		manual("main", loopOf("100", act("run", obj{"automation_id": "child"}))),
		manual("child", loopOf("100", act("run", obj{"automation_id": "leaf"}))),
		manual("leaf", loopOf("100", addVar("c"))),
	)
	fx.run(t, "main", "req-run-budget", nil)
	assert.LessOrEqual(t, len(fx.vars.writesSoFar()), automation.MaxActions)
	assert.NotEmpty(t, fx.vars.writesSoFar())
}

func TestEveryForkedPathResumes(t *testing.T) {
	fork := parallelOf(actions(delayOf(1), addVar("late")), actions(delayOf(1), addVar("late")))
	fx := newAutoFixture(manual("main", loopOf("100", fork)))
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: "req-forks", Source: automationsrpc.SourceDashboard})
	require.True(t, reply.Accepted)
	assert.Eventually(t, func() bool { return len(fx.vars.writesSoFar()) == 200 }, 3*time.Second, 5*time.Millisecond)
	assert.Eventually(t, func() bool { return fx.p.autoExec.running.Load() == 0 }, 3*time.Second, 5*time.Millisecond)
}

func TestSlowStepsMoveOffTheEventLane(t *testing.T) {
	get := act("http_get", obj{"url": "https://example.com/x"})
	fx := newAutoFixture(manual("main", get, get, get, setVar("done", "yes")))
	fx.p.autoSegment = time.Millisecond
	fx.gossip.slow = 3 * time.Millisecond
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: "req-slow", Source: automationsrpc.SourceDashboard})
	require.True(t, reply.Accepted)
	assert.Less(t, fx.gossip.callCount(), 3, "run returned to the lane before the last fetch")
	rec := fx.awaitRecord(t, reply.RunID)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, []string{"done=yes"}, fx.vars.writesSoFar())
}

func (g *autoGossip) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func TestRecordsPerIterationAndTrimmedToTheStoredCap(t *testing.T) {
	branch := act("if", obj{"condition": obj{"type": "live", "params": obj{"state": "live"}}})
	fx := newAutoFixture(manual("main", loopOf("100", loopOf("100", branch))))
	rec := fx.run(t, "main", "req-trim", nil)
	assert.Len(t, rec.Actions, automation.MaxRecordedActions)
	assert.Equal(t, "max_steps", rec.Actions[len(rec.Actions)-1].Detail, "the trimmed record still says how the run ended")
}

func TestRunChildInsideLoopDedupsPerIteration(t *testing.T) {
	fx := newAutoFixture(
		manual("main", loopOf("3", act("run", obj{"automation_id": "child"}))),
		manual("child", setVar("seen", "{local:index}")),
	)
	fx.run(t, "main", "req-run-loop", nil)
	assert.Equal(t, []string{"seen=1", "seen=2", "seen=3"}, fx.vars.writesSoFar())
	fx.run(t, "main", "req-run-loop", nil)
	assert.Len(t, fx.vars.writesSoFar(), 3)
}
