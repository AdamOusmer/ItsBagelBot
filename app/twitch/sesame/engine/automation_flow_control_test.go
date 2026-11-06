// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationMaxActions(t *testing.T) {
	var actions []automation.Action
	for range automation.MaxActions + 1 {
		actions = append(actions, act("clip", obj{}))
	}
	fx := newAutoFixture(manual("main", actions...))
	rec := fx.run(t, "main", "req-max", nil)
	require.Len(t, rec.Actions, automation.MaxActions+1)
	assert.Equal(t, automationrpc.StatusLimited, rec.Actions[automation.MaxActions].Status)
	assert.Equal(t, "max_actions", rec.Actions[automation.MaxActions].Detail)
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
	assert.Len(t, fx.messages(t), automation.MaxActions)
}

func TestAutomationRunRecursionDepth(t *testing.T) {
	fx := newAutoFixture(
		manual("a", act("run", obj{"automation_id": "b"})),
		manual("b", act("run", obj{"automation_id": "c"})),
		manual("c", act("run", obj{"automation_id": "d"})),
		manual("d", act("chat", obj{"text": "deep"})),
	)
	fx.run(t, "a", "req-depth", nil)
	assert.Equal(t, []string{"0:run:limited:depth"}, statuses(fx.runs.forAutomation("c")[0]))
	assert.Empty(t, fx.runs.forAutomation("d"))
	assert.Empty(t, fx.messages(t))
	assert.Equal(t, []string{"0:run:failed:child_failed"}, statuses(fx.runs.forAutomation("b")[0]))
}

func TestAutomationRunsEnabledTargetOnly(t *testing.T) {
	disabled := manual("off", act("chat", obj{"text": "never"}))
	disabled.Enabled = false
	fx := newAutoFixture(
		manual("main", act("run", obj{"automation_id": "child"}), act("run", obj{"automation_id": "off"})),
		manual("child", act("chat", obj{"text": "from child"})),
		disabled,
	)
	rec := fx.run(t, "main", "req-run", nil)
	assert.Equal(t, []string{"0:run:ok:ok", "1:run:failed:not_found"}, statuses(rec))
	assert.Equal(t, []string{`chat {"broadcaster_id":"1","message":"from child"}`}, fx.described(t))
	assert.Len(t, fx.runs.forAutomation("child"), 1)
}

func TestAutomationIfElse(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{{"live", "then"}, {"offline", "else"}}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			branch := act("if", obj{
				"condition": obj{"type": "live", "params": obj{"state": tc.state}},
				"then":      []automation.Action{act("chat", obj{"text": "then"})},
				"else":      []automation.Action{act("chat", obj{"text": "else"})},
			})
			fx := newAutoFixture(manual("main", branch))
			rec := fx.run(t, "main", "req-if-"+tc.state, nil)
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
			assert.Equal(t, []string{fmt.Sprintf(`chat {"broadcaster_id":"1","message":%q}`, tc.want)}, fx.described(t))
			assert.Equal(t, []string{"0:if:ok:", "0:chat:ok:"}, statuses(rec))
		})
	}
}

func TestAutomationRandomPick(t *testing.T) {
	pick := act("random_pick", obj{"branches": []obj{
		{"weight": 1, "actions": []automation.Action{act("chat", obj{"text": "first"})}},
		{"weight": 3, "actions": []automation.Action{act("chat", obj{"text": "second"})}},
	}})
	cases := []struct {
		roll int
		want string
	}{{0, "first"}, {1, "second"}, {3, "second"}}
	for _, tc := range cases {
		t.Run(tc.want+fmt.Sprint(tc.roll), func(t *testing.T) {
			fx := newAutoFixture(manual("main", pick))
			fx.p.autoIntN = func(n int) int {
				require.Equal(t, 4, n)
				return tc.roll
			}
			fx.run(t, "main", "req-pick", nil)
			assert.Equal(t, []string{fmt.Sprintf(`chat {"broadcaster_id":"1","message":%q}`, tc.want)}, fx.described(t))
		})
	}
}

func TestAutomationStopEndsRun(t *testing.T) {
	fx := newAutoFixture(manual("main", act("chat", obj{"text": "a"}), act("stop", obj{}), act("chat", obj{"text": "b"})))
	rec := fx.run(t, "main", "req-stop", nil)
	assert.Equal(t, []string{"0:chat:ok:", "1:stop:ok:"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Len(t, fx.messages(t), 1)
}
