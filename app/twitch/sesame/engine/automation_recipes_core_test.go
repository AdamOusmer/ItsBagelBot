// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type autoVariables struct {
	mu      sync.Mutex
	values  map[string]string
	writes  []string
	limited bool
	getErr  error
}

func newAutoVariables() *autoVariables { return &autoVariables{values: map[string]string{}} }

func variableKey(owner VariableOwner, name string) string {
	return owner.Scope + "/" + strconv.FormatUint(owner.ViewerID, 10) + "/" + name
}

func (v *autoVariables) VariablesGet(_ context.Context, owner VariableOwner, names []string) (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.getErr != nil {
		return nil, v.getErr
	}
	out := map[string]string{}
	for _, name := range names {
		if value, ok := v.values[variableKey(owner, name)]; ok {
			out[name] = value
		}
	}
	return out, nil
}

func (v *autoVariables) VariableSet(_ context.Context, owner VariableOwner, name, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.limited {
		return ErrVariableLimit
	}
	v.values[variableKey(owner, name)] = value
	v.writes = append(v.writes, name+"="+value)
	return nil
}

func (v *autoVariables) VariableAdd(_ context.Context, owner VariableOwner, name string, amount int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	next, _ := automation.AddToVariable(v.values[variableKey(owner, name)], amount)
	v.values[variableKey(owner, name)] = next
	v.writes = append(v.writes, name+"+"+strconv.FormatInt(amount, 10))
	return nil
}

func (v *autoVariables) VariableDelete(_ context.Context, owner VariableOwner, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.values, variableKey(owner, name))
	return nil
}

func (v *autoVariables) writesSoFar() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.writes)
}

func (v *autoVariables) value(owner VariableOwner, name string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.values[variableKey(owner, name)]
}

var channelVars = VariableOwner{BroadcasterID: 1, Scope: automation.VariableScopeChannel}

func (fx *autoFixture) awaitWrites(t *testing.T, count int) []string {
	t.Helper()
	require.Eventually(t, func() bool { return len(fx.vars.writesSoFar()) >= count }, 3*time.Second, 2*time.Millisecond, "writes: %v", fx.vars.writesSoFar())
	return fx.vars.writesSoFar()
}

func actions(list ...automation.Action) []automation.Action { return list }

func loopOf(times string, body ...automation.Action) automation.Action {
	return act("loop", obj{"times": times, "actions": body})
}

func setVar(name, value string) automation.Action {
	return act("var_set", obj{"scope": "channel", "name": name, "value": value})
}

func addVar(name string) automation.Action {
	return act("var_add", obj{"scope": "channel", "name": name, "amount": 1})
}

func delayOf(ms int) automation.Action { return act("delay", obj{"ms": ms}) }

func parallelOf(paths ...[]automation.Action) automation.Action {
	branches := make([]obj, len(paths))
	for i, path := range paths {
		branches[i] = obj{"weight": 1, "actions": path}
	}
	return act("parallel", obj{"branches": branches})
}

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

func TestForkedPathsPerRunAreCapped(t *testing.T) {
	fork := parallelOf(actions(delayOf(1), addVar("late")), actions(delayOf(1), addVar("late")))
	fx := newAutoFixture(manual("main", loopOf("100", fork)))
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: "req-forks", Source: automationsrpc.SourceDashboard})
	require.True(t, reply.Accepted)
	assert.Eventually(t, func() bool { return len(fx.vars.writesSoFar()) == maxForksPerRun }, 3*time.Second, 5*time.Millisecond)
	assert.Eventually(t, func() bool { return fx.p.autoExec.running.Load() == 0 }, 3*time.Second, 5*time.Millisecond)
	assert.True(t, slices.ContainsFunc(fx.runs.records(), func(r automationrpc.RunRecord) bool {
		return slices.Contains(statuses(r), "0:var_add:limited:delay_capacity")
	}))
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

func TestLoopFrameRefusedContinuationIsRecordedLimited(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("2", delayOf(1), addVar("c"))))
	fx.p.autoExec.running.Store(maxContinuations)
	rec := fx.run(t, "main", "req-loop-cap", nil)
	assert.Equal(t, []string{"0:loop:ok:", "0:delay:ok:", "0:var_add:limited:delay_capacity", "0:loop:limited:delay_capacity"}, statuses(rec))
	assert.Empty(t, fx.vars.writesSoFar())
}

func TestRecordsPerIterationAndTrimmedToTheStoredCap(t *testing.T) {
	branch := act("if", obj{"condition": obj{"type": "live", "params": obj{"state": "live"}}})
	fx := newAutoFixture(manual("main", loopOf("100", loopOf("100", branch))))
	rec := fx.run(t, "main", "req-trim", nil)
	assert.Len(t, rec.Actions, automation.MaxRecordedActions)
	assert.Equal(t, "max_steps", rec.Actions[len(rec.Actions)-1].Detail, "the trimmed record still says how the run ended")
}

func saveLocal(name, value string) automation.Action {
	return act("text", obj{"op": "trim", "input": value, "save_as": name})
}

func TestLocalsSurviveADelay(t *testing.T) {
	fx := newAutoFixture(manual("main", saveLocal("x", "kept {arg:a}"), delayOf(1), setVar("out", "{local:x}|{local:X}|{local:missing}")))
	fx.run(t, "main", "req-local-delay", map[string]string{"a": "1"})
	assert.Equal(t, []string{"out=kept 1|kept 1|"}, fx.vars.writesSoFar())
}

func TestLocalsCopyIntoEachParallelPath(t *testing.T) {
	fx := newAutoFixture(manual("main",
		saveLocal("x", "root"),
		parallelOf(
			actions(saveLocal("x", "first"), delayOf(1), setVar("p1", "{local:x}")),
			actions(setVar("p2", "{local:x}"), saveLocal("x", "second"), setVar("p2b", "{local:x}")),
		),
		setVar("after", "{local:x}"),
	))
	fx.run(t, "main", "req-local-par", nil)
	assert.ElementsMatch(t, []string{"p1=first", "p2=root", "p2b=second", "after=root"}, fx.awaitWrites(t, 4))
}

func TestLocalsFlowDownIntoRunChildrenAndNotBack(t *testing.T) {
	fx := newAutoFixture(
		manual("main", saveLocal("x", "parent"), act("run", obj{"automation_id": "child"}), setVar("back", "{local:x}|{local:fromchild}")),
		manual("child", setVar("seen", "{local:x}"), saveLocal("fromchild", "y"), saveLocal("x", "child")),
	)
	fx.run(t, "main", "req-local-run", nil)
	assert.Equal(t, []string{"seen=parent", "back=parent|"}, fx.vars.writesSoFar())
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

func TestStoredContentIsNeverExpandedAgain(t *testing.T) {
	const hostile = "{arg:secret} {var:other} {local:secret} {user}"
	args := map[string]string{"evil": hostile, "secret": "LEAK"}
	cases := map[string][]automation.Action{
		"channel variable": actions(setVar("x", "{arg:evil}"), act("chat", obj{"text": "{var:x}"})),
		"viewer variable": actions(
			act("var_set", obj{"scope": "viewer", "name": "x", "value": "{arg:evil}"}),
			act("chat", obj{"text": "{viewervar:x}"}),
		),
		"variable copied to a variable": actions(setVar("x", "{arg:evil}"), setVar("y", "{var:x}"), act("chat", obj{"text": "{var:y}"})),
		"local":                         actions(saveLocal("x", "{arg:evil}"), act("chat", obj{"text": "{local:x}"})),
		"local kept in a variable":      actions(saveLocal("x", "{arg:evil}"), setVar("y", "{local:x}"), act("chat", obj{"text": "{var:y}"})),
		"local across a delay":          actions(saveLocal("x", "{arg:evil}"), delayOf(1), act("chat", obj{"text": "{local:x}"})),
		"fetch result": actions(
			act("http_get", obj{"url": "https://example.com/x", "save_as": "x"}),
			act("chat", obj{"text": "{local:x}"}),
		),
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", steps...))
			fx.gossip.values = []string{hostile}
			fx.run(t, "main", "req-reexpand", args)
			assert.Equal(t, []string{hostile}, fx.pub.chatTexts(t))
		})
	}
}

func TestStoredTemplateIsLiteralInConditionsToo(t *testing.T) {
	branch := act("if", obj{
		"condition": obj{"type": "var", "params": obj{"scope": "channel", "name": "x", "op": "eq", "value": "{arg:evil}"}},
		"then":      actions(setVar("result", "same")),
		"else":      actions(setVar("result", "different")),
	})
	fx := newAutoFixture(manual("main", setVar("x", "{arg:evil}"), branch))
	fx.run(t, "main", "req-cond-literal", map[string]string{"evil": "{arg:secret}", "secret": "LEAK"})
	assert.Equal(t, "same", fx.vars.value(channelVars, "result"))
	assert.Equal(t, "{arg:secret}", fx.vars.value(channelVars, "x"))
}

func TestVariableSteps(t *testing.T) {
	fx := newAutoFixture(manual("main",
		act("var_set", obj{"scope": "channel", "name": "Deaths", "value": "{arg:n}"}),
		act("var_add", obj{"scope": "channel", "name": "deaths", "amount": 2}),
		act("var_set", obj{"scope": "viewer", "name": "mine", "value": "v"}),
		setVar("read", "{var:DEATHS}/{viewervar:mine}/{var:absent}/{viewervar:absent}"),
		act("var_delete", obj{"scope": "channel", "name": "deaths"}),
		setVar("gone", "[{var:deaths}]"),
	))
	rec := fx.run(t, "main", "req-vars", map[string]string{"n": "5"})
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, "7/v//", fx.vars.value(channelVars, "read"))
	assert.Equal(t, "[]", fx.vars.value(channelVars, "gone"))
}

func TestVariableAddIsNotRepeatedByRedelivery(t *testing.T) {
	fx := newAutoFixture(manual("main", addVar("c")))
	fx.run(t, "main", "req-add", nil)
	second := fx.run(t, "main", "req-add", nil)
	assert.Equal(t, "1", fx.vars.value(channelVars, "c"))
	assert.Equal(t, []string{"0:var_add:skipped:duplicate"}, statuses(second))
}

func TestVariableLimitsAreStepStatusLimited(t *testing.T) {
	over := setVar("x", "{arg:long}")
	fx := newAutoFixture(manual("main", over, setVar("y", "fine")))
	rec := fx.run(t, "main", "req-long", map[string]string{"long": strings.Repeat("é", automation.MaxVariableValueRunes+1)})
	assert.Equal(t, []string{"0:var_set:limited:var_limit", "1:var_set:ok:"}, statuses(rec))

	fx = newAutoFixture(manual("main", setVar("x", "a")))
	fx.vars.limited = true
	rec = fx.run(t, "main", "req-names", nil)
	assert.Equal(t, []string{"0:var_set:limited:var_limit"}, statuses(rec))
}

func TestViewerVariableNeedsAViewer(t *testing.T) {
	fx := newAutoFixture(manual("main", act("var_set", obj{"scope": "viewer", "name": "x", "value": "a"}), setVar("y", "[{viewervar:x}]")))
	reply := fx.p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "main", RequestID: "req-viewer", Source: automationsrpc.SourceDashboard})
	require.True(t, reply.Accepted)
	rec := fx.awaitRecord(t, reply.RunID)
	assert.Equal(t, []string{"0:var_set:ok:", "1:var_set:ok:"}, statuses(rec), "dashboard runs act as the broadcaster")

	timer := newAutoFixture(manual("main", act("var_add", obj{"scope": "viewer", "name": "x", "amount": 1})))
	plan := automation.NewPlan(manual("main", act("var_add", obj{"scope": "viewer", "name": "x", "amount": 1})))
	run := &AutomationRun{Plan: plan, BroadcasterID: 1, ID: "r", Identity: "i"}
	timer.p.runAutomation(context.Background(), run, func(*module.Output) {})
	assert.Equal(t, []string{"0:var_add:failed:no_viewer"}, statuses(run.Record))
}

func TestVariableStoreFailureIsReportedAndTokensRenderEmpty(t *testing.T) {
	fx := newAutoFixture(manual("main", act("chat", obj{"text": "x[{var:a}]"})))
	fx.vars.getErr = errors.New("down")
	rec := fx.run(t, "main", "req-get-err", nil)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Equal(t, []string{"x[]"}, fx.pub.chatTexts(t))
}

func TestVarCondition(t *testing.T) {
	cases := []struct {
		name    string
		stored  string
		op      string
		value   string
		passes  bool
		missing bool
	}{
		{name: "numeric eq", stored: "10", op: "eq", value: "10.0", passes: true},
		{name: "numeric gt is not lexical", stored: "10", op: "gt", value: "9", passes: true},
		{name: "text gt", stored: "b", op: "gt", value: "a", passes: true},
		{name: "mixed is text", stored: "10", op: "lt", value: "a", passes: true},
		{name: "ne", stored: "a", op: "ne", value: "b", passes: true},
		{name: "contains", stored: "hello", op: "contains", value: "ell", passes: true},
		{name: "contains empty", stored: "hello", op: "contains", value: "", passes: false},
		{name: "exists", stored: "", op: "exists", passes: true},
		{name: "missing exists", missing: true, op: "exists", passes: false},
		{name: "missing is empty", missing: true, op: "eq", value: "", passes: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auto := manual("main", act("chat", obj{"text": "ran"}))
			auto.Definition.Conditions = []automation.Condition{{Type: "var", Params: rawJSON(obj{"scope": "channel", "name": "x", "op": tc.op, "value": tc.value})}}
			fx := newAutoFixture(auto)
			if !tc.missing {
				fx.vars.values[variableKey(channelVars, "x")] = tc.stored
			}
			rec := fx.run(t, "main", "req-cond", nil)
			want := automationrpc.OutcomeSkipped
			if tc.passes {
				want = automationrpc.OutcomeOK
			}
			assert.Equal(t, want, rec.Outcome)
		})
	}
}

func TestVarConditionStoreErrorFailsTheRun(t *testing.T) {
	auto := manual("main", act("chat", obj{"text": "ran"}))
	auto.Definition.Conditions = []automation.Condition{{Type: "var", Params: rawJSON(obj{"scope": "channel", "name": "x", "op": "exists"})}}
	fx := newAutoFixture(auto)
	fx.vars.getErr = errors.New("down")
	rec := fx.run(t, "main", "req-cond-err", nil)
	assert.Equal(t, automationrpc.OutcomeFailed, rec.Outcome)
	assert.Empty(t, fx.messages(t))
}

func TestTextStepsOnHostileInput(t *testing.T) {
	huge := strings.Repeat("é", 200_000)
	args := map[string]string{"huge": huge, "n": "1"}
	cases := []struct {
		name   string
		params obj
		status string
		want   string
	}{
		{"upper", obj{"op": "upper", "input": "aé"}, "ok", "AÉ"},
		{"length counts runes", obj{"op": "length", "input": "{arg:huge}"}, "ok", "200000"},
		{"huge replace", obj{"op": "replace", "input": "{arg:huge}", "arg1": "é", "arg2": "{arg:huge}"}, "ok", strings.Repeat("é", automation.MaxSavedValueRunes)},
		{"empty separator splits runes", obj{"op": "split_pick", "input": "aéb", "arg1": "", "arg2": "2"}, "ok", "é"},
		{"split index past the end", obj{"op": "split_pick", "input": "a,b", "arg1": ",", "arg2": "9"}, "ok", ""},
		{"split index zero", obj{"op": "split_pick", "input": "a,b", "arg1": ",", "arg2": "0"}, "ok", ""},
		{"split bad index", obj{"op": "split_pick", "input": "a,b", "arg1": ",", "arg2": "x"}, "failed", ""},
		{"substring past the end", obj{"op": "substring", "input": "abc", "arg1": "99", "arg2": "1"}, "ok", ""},
		{"substring negative start", obj{"op": "substring", "input": "abc", "arg1": "-5", "arg2": "2"}, "ok", "ab"},
		{"substring huge length", obj{"op": "substring", "input": "abc", "arg1": "1", "arg2": "9223372036854775807"}, "ok", "bc"},
		{"substring in runes", obj{"op": "substring", "input": "éàb", "arg1": "1", "arg2": "1"}, "ok", "à"},
		{"substring bad start", obj{"op": "substring", "input": "abc", "arg1": "x", "arg2": "1"}, "failed", ""},
		{"substring overflowing start", obj{"op": "substring", "input": "abc", "arg1": "99999999999999999999", "arg2": "1"}, "failed", ""},
		{"contains", obj{"op": "contains", "input": "abc", "arg1": "b"}, "ok", "true"},
		{"url encode", obj{"op": "url_encode", "input": "a b&c"}, "ok", "a+b%26c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.params["save_as"] = "out"
			fx := newAutoFixture(manual("main", act("text", tc.params), setVar("out", "{local:out}")))
			rec := fx.run(t, "main", "req-text", args)
			assert.Equal(t, tc.status, rec.Actions[0].Status, statuses(rec))
			assert.Equal(t, tc.want, fx.vars.value(channelVars, "out"))
		})
	}
}

func TestMathStepsOnHostileInput(t *testing.T) {
	deep := strings.Repeat("(", 98) + "1" + strings.Repeat(")", 98)
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"precedence", "1 + 2 * 3 ^ 2", "19"},
		{"parentheses", "(1 + 2) * 3", "9"},
		{"unary", "-2 ^ 2", "-4"},
		{"right associative power", "2 ^ 3 ^ 2", "512"},
		{"modulo", "10 % 3", "1"},
		{"decimal", "1 / 4", "0.25"},
		{"integer has no point", "10 / 2", "5"},
		{"negative zero", "-0", "0"},
		{"functions", "max(round(1.5), floor(2.9), ceil(0.1), abs(-1), min(9, 8))", "8"},
		{"token first", "{arg:n} * 2", "42"},
		{"random lower bound with fixed roll", "random(5, 9)", "5"},
		{"random swaps bounds", "random(9, 5)", "5"},
		{"deep but in limit", deep, "1"},
		{"float64 precision is the contract", "2 ^ 64", "18446744073709552000"},
		{"unknown function", "evil(1)", ""},
		{"divide by zero", "1 / 0", ""},
		{"modulo by zero", "1 % 0", ""},
		{"parse error", "1 +", ""},
		{"trailing text", "1 2", ""},
		{"empty", "{arg:absent}", ""},
		{"overflow", "9 ^ 9 ^ 9 ^ 9", ""},
		{"overflow by product", "10 ^ 308 * 10", ""},
		{"nan", "(0 - 1) ^ 0.5", ""},
		{"random range overflow", "random(-1 * 10 ^ 308, 10 ^ 308)", ""},
		{"too many parentheses", strings.Repeat("(", 150) + "1" + strings.Repeat(")", 150), ""},
		{"unbalanced parentheses", strings.Repeat("(", 150), ""},
		{"wrong argument count", "abs(1, 2)", ""},
		{"no arguments", "max()", ""},
		{"code", "os.exit(1)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("math", obj{"expression": tc.expr, "save_as": "out"}), setVar("out", "[{local:out}]")))
			fx.p.autoIntN = func(int) int { return 0 }
			rec := fx.run(t, "main", "req-math", map[string]string{"n": "21"})
			if tc.want == "" {
				assert.Equal(t, "failed", rec.Actions[0].Status, statuses(rec))
				assert.Equal(t, "math_error", rec.Actions[0].Detail)
				assert.Equal(t, "[]", fx.vars.value(channelVars, "out"), "failure saves nothing")
				return
			}
			assert.Equal(t, "ok", rec.Actions[0].Status, statuses(rec))
			assert.Equal(t, "["+tc.want+"]", fx.vars.value(channelVars, "out"))
		})
	}
}

func fetchStatuses() []gossiprpc.FetchStatus {
	return []gossiprpc.FetchStatus{
		gossiprpc.FetchDenied, gossiprpc.FetchLimited, gossiprpc.FetchUpstreamError, gossiprpc.FetchTimeout, gossiprpc.FetchBadDef,
	}
}

func TestHTTPGetSavesAndRunsOnOK(t *testing.T) {
	fx := newAutoFixture(manual("main",
		act("http_get", obj{
			"url": "https://example.com/{arg:path}", "json_path": []string{"a", "b"}, "save_as": "Result",
			"on_ok": actions(setVar("ok", "{local:result}")), "on_failure": actions(setVar("bad", "x")),
		}),
		setVar("after", "{local:result}"),
	))
	fx.gossip.values = []string{strings.Repeat("é", 600), "second"}
	rec := fx.run(t, "main", "req-get", map[string]string{"path": "p"})
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, GossipRoute{Provider: "custom", Endpoint: "fetch"}, fx.gossip.route)
	assert.Equal(t, &gossiprpc.FetchDef{Name: "get:1", URL: "https://example.com/p", JSONPath: []string{"a", "b"}, IsActive: true}, fx.gossip.req.Def)
	assert.Equal(t, strings.Repeat("é", automation.MaxSavedValueRunes), fx.vars.value(channelVars, "ok"))
	assert.Equal(t, strings.Repeat("é", automation.MaxSavedValueRunes), fx.vars.value(channelVars, "after"))
	assert.Empty(t, fx.vars.value(channelVars, "bad"))
}

func TestHTTPGetEveryFailureRunsOnFailureAndStepsAfter(t *testing.T) {
	for _, status := range fetchStatuses() {
		t.Run(string(status), func(t *testing.T) {
			fx := newAutoFixture(manual("main",
				act("http_get", obj{"url": "https://example.com/", "save_as": "r", "on_ok": actions(setVar("ok", "x")), "on_failure": actions(setVar("why", "{local:error}"))}),
				setVar("after", "[{local:r}]{local:error}"),
			))
			fx.gossip.status = status
			rec := fx.run(t, "main", "req-get-fail", nil)
			assert.Equal(t, []string{"0:http_get:failed:" + string(status), "0:var_set:ok:", "1:var_set:ok:"}, statuses(rec))
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, "a handled failure is not a failed run")
			assert.Equal(t, string(status), fx.vars.value(channelVars, "why"))
			assert.Equal(t, "[]"+string(status), fx.vars.value(channelVars, "after"))
			assert.Empty(t, fx.vars.value(channelVars, "ok"))
		})
	}
}

func TestFetchOutcomeRule(t *testing.T) {
	cases := []struct {
		name   string
		node   string
		params obj
		after  []automation.Action
		want   string
	}{
		{name: "get without a path fails the run", node: "http_get", params: obj{}, want: automationrpc.OutcomeFailed},
		{name: "get without a path with other steps is partial", node: "http_get", params: obj{}, after: actions(setVar("a", "b")), want: automationrpc.OutcomePartial},
		{name: "get handled is ok", node: "http_get", params: obj{"on_failure": actions(setVar("a", "b"))}, want: automationrpc.OutcomeOK},
		{name: "post handled is ok", node: "http_post", params: obj{"on_failure": actions(setVar("a", "b"))}, want: automationrpc.OutcomeOK},
		{name: "get whose path also fails stays failed", node: "http_get", params: obj{"on_failure": actions(act("math", obj{"expression": "1/0", "save_as": "m"}))}, want: automationrpc.OutcomeFailed},
		{name: "disabled path did not handle it", node: "http_get", params: obj{"on_failure": actions(disabledAction(setVar("a", "b")))}, want: automationrpc.OutcomeFailed},
		{name: "handled failure next to a real one is partial", node: "http_get", params: obj{"on_failure": actions(setVar("a", "b"))}, after: actions(act("math", obj{"expression": "1/0", "save_as": "m"})), want: automationrpc.OutcomePartial},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.params["url"] = "https://example.com/"
			fx := newAutoFixture(manual("main", append(actions(act(tc.node, tc.params)), tc.after...)...))
			fx.gossip.status = gossiprpc.FetchTimeout
			rec := fx.run(t, "main", "req-outcome", nil)
			assert.Equal(t, tc.want, rec.Outcome, statuses(rec))
			assert.Equal(t, automationrpc.StatusFailed, rec.Actions[0].Status)
		})
	}
}

func TestHandledFailureSurvivesADelayInItsPath(t *testing.T) {
	fx := newAutoFixture(manual("main", act("http_get", obj{"url": "https://example.com/", "on_failure": actions(delayOf(1), setVar("a", "{local:error}"))})))
	fx.gossip.status = gossiprpc.FetchDenied
	rec := fx.run(t, "main", "req-handled-delay", nil)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
	assert.Equal(t, "denied", fx.vars.value(channelVars, "a"))
}

func TestHTTPGetTransportAndURLFailures(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		err   error
		want  string
		calls int
	}{
		{"gossip down", "https://example.com/", errors.New("no responders"), "gossip_error", 1},
		{"rendered http", "{arg:u}", nil, "bad_def", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act("http_get", obj{"url": tc.url, "on_failure": actions(setVar("why", "{local:error}"))})))
			fx.gossip.err = tc.err
			fx.run(t, "main", "req-get-url", map[string]string{"u": "http://example.com/"})
			assert.Equal(t, tc.want, fx.vars.value(channelVars, "why"))
			assert.Equal(t, tc.calls, fx.gossip.calls)
		})
	}
	for _, private := range []string{"https://localhost/x", "https://10.0.0.1/x", "https://metadata.internal/x"} {
		fx := newAutoFixture(manual("main", act("http_get", obj{"url": "{arg:u}"})))
		rec := fx.run(t, "main", "req-get-private", map[string]string{"u": private})
		assert.Equal(t, []string{"0:http_get:failed:bad_def"}, statuses(rec), private)
		assert.Zero(t, fx.gossip.calls, private)
	}
}

func TestHTTPGetRepeatsOnRedeliveryAndPostDoesNot(t *testing.T) {
	fx := newAutoFixture(manual("main", act("http_get", obj{"url": "https://example.com/"})))
	fx.run(t, "main", "req-get-again", nil)
	fx.run(t, "main", "req-get-again", nil)
	assert.Equal(t, 2, fx.gossip.calls)

	fx = newAutoFixture(manual("main", act("http_post", obj{"url": "https://example.com/", "on_ok": actions(setVar("ok", "x"))})))
	fx.run(t, "main", "req-post-again", nil)
	second := fx.run(t, "main", "req-post-again", nil)
	assert.Equal(t, 1, fx.gossip.calls)
	assert.Equal(t, []string{"0:http_post:skipped:duplicate"}, statuses(second))
	assert.Equal(t, []string{"ok=x"}, fx.vars.writesSoFar())
}

func TestHTTPPostFailurePathAndResponse(t *testing.T) {
	saving := obj{"url": "https://example.com/", "save_as": "r", "on_ok": actions(setVar("ok", "{local:r}")), "on_failure": actions(setVar("why", "{local:error}"))}
	fx := newAutoFixture(manual("main", act("http_post", saving)))
	fx.gossip.values = []string{"created"}
	fx.run(t, "main", "req-post-ok", nil)
	assert.True(t, fx.gossip.req.CaptureBody)
	assert.Equal(t, "created", fx.vars.value(channelVars, "ok"))

	for _, status := range fetchStatuses() {
		fx := newAutoFixture(manual("main", act("http_post", saving)))
		fx.gossip.status = status
		rec := fx.run(t, "main", "req-post-fail", nil)
		assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
		assert.Equal(t, "0:http_post:failed:"+string(status), statuses(rec)[0])
		assert.Equal(t, string(status), fx.vars.value(channelVars, "why"))
	}
}

func TestHTTPPostInsideLoopPostsEachIterationOnce(t *testing.T) {
	fx := newAutoFixture(manual("main", loopOf("3", act("http_post", obj{"url": "https://example.com/", "body": "{local:index}"}))))
	fx.run(t, "main", "req-post-loop", nil)
	assert.Equal(t, 3, fx.gossip.calls)
	assert.Equal(t, "3", fx.gossip.req.Body)
	fx.run(t, "main", "req-post-loop", nil)
	assert.Equal(t, 3, fx.gossip.calls)
}

func TestFetchNodeConsumesBudget(t *testing.T) {
	fx := newAutoFixture(manual("main", act("http_get", obj{"url": "https://example.com/"})))
	x := fx.p.newActionExec(context.Background(), &AutomationRun{Plan: automation.NewPlan(manual("main")), BroadcasterID: 1, ID: "r"}, func(*module.Output) {})
	defer releaseActionExec(x)
	x.budget.executed = automation.MaxActions
	plan := automation.NewPlan(manual("main", act("http_get", obj{"url": "https://example.com/"})))
	x.execList(plan.Steps)
	assert.Equal(t, []string{"0:http_get:limited:max_actions"}, statuses(x.run.Record))
	assert.Zero(t, fx.gossip.calls)
}

func TestManyVariableTokensReachTheStoreOnce(t *testing.T) {
	var parts []string
	for i := range 10 {
		parts = append(parts, fmt.Sprintf("{var:n%d}", i))
	}
	fx := newAutoFixture(manual("main", act("chat", obj{"text": strings.Join(parts, "")})))
	for i := range 10 {
		fx.vars.values[variableKey(channelVars, fmt.Sprintf("n%d", i))] = strconv.Itoa(i)
	}
	fx.run(t, "main", "req-many", nil)
	assert.Equal(t, []string{"0123456789"}, fx.pub.chatTexts(t))
}
