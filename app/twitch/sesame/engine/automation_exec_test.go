// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationOutputActions(t *testing.T) {
	args := map[string]string{"a": "hello", "k": "v"}
	cases := []struct {
		name   string
		action automation.Action
		want   []string
	}{
		{"chat expands args", act("chat", obj{"text": "hi {args} {arg:k}"}), []string{`chat {"broadcaster_id":"1","message":"hi hello v v"}`}},
		{"chat multi-line is a batch", act("chat", obj{"text": "one\n\ntwo"}), []string{`chat {"broadcaster_id":"1","message":"one"}`, `chat {"broadcaster_id":"1","message":"two"}`}},
		{"announce color", act("announce", obj{"text": "big news", "color": "green"}), []string{`announce color=green {"message":"big news"}`}},
		{"raid", act("raid", obj{"login": "target"}), []string{`raid_start to=target {}`}},
		{"timeout", act("timeout", obj{"login": "@Target", "seconds": 60, "reason": "spam {args}"}), []string{`timeout {"data":{"user_id":"99","duration":60,"reason":"spam hello v"}}`}},
		{"ban", act("ban", obj{"login": "target", "reason": "bye"}), []string{`ban {"data":{"user_id":"99","reason":"bye"}}`}},
		{"ban truncates expanded Unicode reason", act("ban", obj{"login": "target", "reason": strings.Repeat("界", 494) + "{args}"}), []string{`ban {"data":{"user_id":"99","reason":"` + strings.Repeat("界", 494) + `hello "}}`}},
		{"warn", act("warn", obj{"login": "target", "reason": "careful"}), []string{`warn {"data":{"user_id":"99","reason":"careful"}}`}},
		{"shield on", act("shield", obj{"on": true}), []string{`shield_mode {"is_active":true}`}},
		{"shield off", act("shield", obj{"on": false}), []string{`shield_mode {"is_active":false}`}},
		{"shoutout", act("shoutout", obj{"login": "@Target"}), []string{`shoutout to=target {}`}},
		{"clip", act("clip", obj{}), []string{`clip {}`}},
		{"marker", act("marker", obj{"description": "moment {args}"}), []string{`stream_marker {"description":"moment hello v"}`}},
		{"set_title", act("set_title", obj{"title": "new {arg:a}"}), []string{`channel_update {"field":"title","value":"new hello"}`}},
		{"set_game", act("set_game", obj{"game": "Chess"}), []string{`channel_update {"field":"game","value":"Chess"}`}},
		{"set_tags", act("set_tags", obj{"tags": "a, b"}), []string{`channel_update {"field":"tags","value":"a, b"}`}},
		{"commercial", act("commercial", obj{"seconds": 60}), []string{`commercial {"length":60}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", tc.action))
			rec := fx.run(t, "main", "req-"+tc.name, args)
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
			assert.Equal(t, tc.want, fx.described(t))
			status := automationrpc.StatusSent
			if tc.action.Type == "chat" {
				status = automationrpc.StatusOK
			}
			assert.Equal(t, []automationrpc.ActionRecord{{Type: tc.action.Type, Status: status}}, rec.Actions)
		})
	}
}

func TestAutomationTargetModes(t *testing.T) {
	cases := []struct {
		name       string
		target     automation.TargetAction
		id         uint64
		login      string
		roleTarget string
	}{
		{"viewer", automation.TargetAction{Who: "viewer", Login: "target"}, 999, "alice", "target_user_id=999"},
		{"mention", automation.TargetAction{Who: "mention", Login: "target"}, 555, "first", "target_user_id=555"},
		{"user", automation.TargetAction{Who: "user", UserID: "00666", Login: " @Picked "}, 666, "picked", "target_user_id=666"},
		{"user without login", automation.TargetAction{Who: "user", UserID: "00666"}, 666, "", "target_user_id=666"},
		{"login", automation.TargetAction{Who: "login", Login: "@Target"}, 99, "target", "to=target"},
		{"legacy login", automation.TargetAction{Login: "@Target"}, 99, "target", "to=target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := targetCommand(
				act("timeout", automation.TimeoutAction{TargetAction: tc.target, Seconds: 60}),
				act("ban", automation.BanAction{TargetAction: tc.target}),
				act("mod", automation.LoginToggleAction{TargetAction: tc.target, On: true}),
				act("vip", automation.LoginToggleAction{TargetAction: tc.target, On: false}),
				act("points_add", automation.PointsAddAction{TargetAction: tc.target, Amount: 5}),
				act("points_set", automation.PointsSetAction{TargetAction: tc.target, Amount: 20}),
			)
			fx := autoFixture{pub: &fakePublisher{}, loyalty: &autoLoyalty{}}
			p, runs := autoRig{automations: []automation.Automation{a}, pub: fx.pub, loyalty: fx.loyalty,
				accounts: autoAccounts{"target": "99"}}.pipeline()
			env := chatEnv("!to @First @Second", "broadcaster")
			env.Mentions = []lane.Mention{{UserID: "555", UserLogin: "First"}, {UserID: "777", UserLogin: "Second"}}

			process(t, p, env)

			assert.Equal(t, []string{
				fmt.Sprintf(`timeout {"data":{"user_id":"%d","duration":60}}`, tc.id),
				fmt.Sprintf(`ban {"data":{"user_id":"%d"}}`, tc.id),
				"mod_add " + tc.roleTarget + " {}", "vip_remove " + tc.roleTarget + " {}",
			}, fx.described(t))
			if tc.login == "" {
				require.Len(t, runs.recs, 1)
				assert.Subset(t, statuses(runs.recs[0]), []string{"4:points_add:failed:empty_login", "5:points_set:failed:empty_login"})
				assert.Empty(t, fx.loyalty.adjusts)
				return
			}
			assert.Equal(t, []BalanceAdjustment{
				{BroadcasterID: 123, ViewerID: tc.id, ViewerLogin: tc.login, Value: 5},
				{BroadcasterID: 123, ViewerID: tc.id, ViewerLogin: tc.login, Value: 20, Absolute: true},
			}, fx.loyalty.adjusts)
		})
	}
}

func targetCommand(actions ...automation.Action) automation.Automation {
	a := command("!to", obj{}, cond("perm", obj{"level": "lead_mod"}))
	a.Definition.Actions = actions
	return a
}

func TestAutomationCommandTargetFailures(t *testing.T) {
	cases := []struct {
		name     string
		target   automation.TargetAction
		mentions []lane.Mention
		want     string
	}{
		{"no mention", automation.TargetAction{Who: "mention"}, nil, "no_target"},
		{"invalid first mention", automation.TargetAction{Who: "mention"},
			[]lane.Mention{{UserID: "bogus"}, {UserID: "555"}}, "no_target"},
		{"zero fixed id", automation.TargetAction{Who: "user", UserID: "0"}, nil, "no_target"},
		{"overflowing fixed id", automation.TargetAction{Who: "user", UserID: "18446744073709551616"}, nil, "no_target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := targetCommand(act("timeout", automation.TimeoutAction{TargetAction: tc.target, Seconds: 60}))
			pub := &fakePublisher{}
			p, runs := autoRig{automations: []automation.Automation{a}, pub: pub}.pipeline()
			env := chatEnv("!to", "broadcaster")
			env.Mentions = tc.mentions

			process(t, p, env)

			require.Len(t, runs.recs, 1)
			assert.Equal(t, []string{"0:timeout:failed:" + tc.want}, statuses(runs.recs[0]))
			assert.Empty(t, pub.snapshot())
		})
	}
}

func TestAutomationModerationRequiresRunData(t *testing.T) {
	cases := []struct {
		name   string
		action automation.Action
		want   string
	}{
		{"viewerless event", act("ban", obj{"who": "viewer"}), "0:ban:failed:no_viewer"},
		{"warn reason expands to empty", act("warn", obj{"login": "target", "reason": "{args}"}), "0:warn:failed:empty_reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testAutomation("target", "event", obj{"kind": "stream.online"})
			a.Definition.Actions = []automation.Action{tc.action}
			pub := &fakePublisher{}
			rig := autoRig{automations: []automation.Automation{a}, pub: pub, accounts: autoAccounts{"target": "99"}}
			p, runs := rig.pipeline()

			process(t, p, eventEnv("stream.online", `{}`))

			require.Len(t, runs.recs, 1)
			assert.Equal(t, []string{tc.want}, statuses(runs.recs[0]))
			assert.Empty(t, pub.snapshot())
		})
	}
}

func TestAModerationTargetWithControlCharactersIsRefused(t *testing.T) {
	fx := newAutoFixture(manual("main", act("ban", obj{"login": "target\nsecond", "reason": "bye"})))

	rec := fx.run(t, "main", "req-control", nil)

	assert.Equal(t, "failed", rec.Actions[0].Status, statuses(rec))
	assert.Equal(t, "empty_login", rec.Actions[0].Detail)
	assert.Empty(t, fx.described(t))
}

func TestAutomationQueuedStepCountsAsSuccess(t *testing.T) {
	fx := newAutoFixture(manual("main", act("raid", obj{"login": "target"}), act("delete_message", obj{})))
	rec := fx.run(t, "main", "mixed-output-result", nil)
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome, statuses(rec))
}

func TestGossipCallsKeepTheRefusalCode(t *testing.T) {
	nc := bustest.NATS(t)
	refuse := func(msg *nats.Msg) { _ = msg.Respond([]byte(`{"error":"gone","code":"not_found"}`)) }
	_, err := nc.Subscribe(gossiprpc.Subject("g", customFetchProvider, customFetchEndpoint), refuse)
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	rpc := NewGossipRPC(nc, "g")

	_, err = rpc.Fetch(context.Background(), gossiprpc.Request{})
	assert.True(t, bus.NotFoundReply(err), "%v", err)

	var reply gossiprpc.CustomFetchReply
	err = rpc.Call(context.Background(), GossipRoute{Provider: customFetchProvider, Endpoint: customFetchEndpoint}, gossiprpc.Request{}, &reply)
	assert.True(t, bus.NotFoundReply(err), "%v", err)
}

func TestAutomationBatchesConsecutiveOutputs(t *testing.T) {
	fx := newAutoFixture(manual("main", act("chat", obj{"text": "one"}), act("shield", obj{"on": true}), act("chat", obj{"text": "two"})))
	fx.run(t, "main", "req-batch", nil)
	snap := fx.pub.snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, outgress.TypeBatch, snap[0].msg.Type)
	var batch outgress.Batch
	require.NoError(t, codec.Unmarshal(snap[0].msg.Payload, &batch))
	assert.Equal(t, "rpc:req-batch>main:0:1", batch.ID, "a redelivered event must resume the batch, not start it again")
	assert.Len(t, batch.Items, 3)
}

func TestAutomationDisabledActionIsSkipped(t *testing.T) {
	fx := newAutoFixture(manual("main", disabledAction(act("chat", obj{"text": "no"})), act("chat", obj{"text": "yes"})))
	rec := fx.run(t, "main", "req-disabled", nil)
	assert.Equal(t, []string{"0:chat:skipped:disabled", "1:chat:ok:"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Len(t, fx.messages(t), 1)
}

func TestEveryChatStepReachesOutgress(t *testing.T) {
	var actions []automation.Action
	for range 4 {
		actions = append(actions, act("chat", obj{"text": "line"}))
	}
	fx := newAutoFixture(manual("main", actions...))
	rec := fx.run(t, "main", "req-chat", nil)
	assert.Equal(t, []string{"0:chat:ok:", "1:chat:ok:", "2:chat:ok:", "3:chat:ok:"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Len(t, fx.messages(t), 4)
}
