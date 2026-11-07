// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
