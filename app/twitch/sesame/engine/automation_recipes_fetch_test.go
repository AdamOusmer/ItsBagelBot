// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
)

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
	x := fx.p.newActionExec(context.Background(), &AutomationRun{Plan: automation.NewPlan(manual("main")), BroadcasterID: 1, ID: "r", Tree: new(runTree)}, func(*module.Output) {})
	defer releaseActionExec(x)
	x.run.Tree.executed.Store(automation.MaxActions)
	plan := automation.NewPlan(manual("main", act("http_get", obj{"url": "https://example.com/"})))
	x.execList(plan.Steps)
	assert.Equal(t, []string{"0:http_get:limited:max_actions"}, statuses(x.run.Record))
	assert.Zero(t, fx.gossip.calls)
}
