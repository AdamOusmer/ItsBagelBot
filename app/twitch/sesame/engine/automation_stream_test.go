// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
)

func TestAutomationStreamControlActions(t *testing.T) {
	args := map[string]string{"a": "Target"}
	cases := []struct {
		name   string
		action automation.Action
		want   string
	}{
		{"poll", act("poll", obj{"title": "Best {arg:a}", "choices": []string{"x", "y"}, "seconds": 60}), `poll_create {"title":"Best Target","choices":["x","y"],"duration":60}`},
		{"poll end terminate", act("poll_end", obj{"status": "terminate"}), `poll_end {"status":"TERMINATED"}`},
		{"poll end archive", act("poll_end", obj{"status": "archive"}), `poll_end {"status":"ARCHIVED"}`},
		{"prediction", act("prediction", obj{"title": "Win?", "outcomes": []string{"yes", "no"}, "seconds": 120}), `prediction_create {"title":"Win?","outcomes":["yes","no"],"window":120}`},
		{"prediction lock", act("prediction_end", obj{"status": "lock"}), `prediction_end {"status":"LOCKED"}`},
		{"prediction cancel", act("prediction_end", obj{"status": "cancel", "winner": 3}), `prediction_end {"status":"CANCELED"}`},
		{"prediction resolve", act("prediction_end", obj{"status": "resolve", "winner": 2}), `prediction_end {"status":"RESOLVED","winner":2}`},
		{"raid", act("raid", obj{"login": "@{arg:a}"}), `raid_start to=target {}`},
		{"raid cancel", act("raid_cancel", obj{}), `raid_cancel {}`},
		{"mod add", act("mod", obj{"login": "Target", "on": true}), `mod_add to=target {}`},
		{"mod remove", act("mod", obj{"login": "Target", "on": false}), `mod_remove to=target {}`},
		{"vip add", act("vip", obj{"login": "target", "on": true}), `vip_add to=target {}`},
		{"vip remove", act("vip", obj{"login": "target", "on": false}), `vip_remove to=target {}`},
		{"slow on", act("chat_mode", obj{"mode": "slow", "on": true, "value": 30}), `chat_settings {"mode":"slow","enabled":true,"duration":30}`},
		{"followers on", act("chat_mode", obj{"mode": "followers", "on": true, "value": 10}), `chat_settings {"mode":"follower","enabled":true,"duration":10}`},
		{"emote off", act("chat_mode", obj{"mode": "emote_only", "on": false, "value": 5}), `chat_settings {"mode":"emote","enabled":false}`},
		{"subscribers on", act("chat_mode", obj{"mode": "subscribers", "on": true}), `chat_settings {"mode":"subscriber","enabled":true}`},
		{"unique on", act("chat_mode", obj{"mode": "unique", "on": true}), `chat_settings {"mode":"unique","enabled":true}`},
		{"clear chat", act("clear_chat", obj{}), `clear_chat {}`},
		{"ad snooze", act("ad_snooze", obj{}), `ad_snooze {}`},
		{"whisper", act("whisper", obj{"login": "Target", "text": "hi {arg:a}"}), `whisper to=target {"message":"hi Target"}`},
		{"branded content on", act("branded_content", obj{"on": true}), `branded_content {"enabled":true}`},
		{"branded content off", act("branded_content", obj{"on": false}), `branded_content {"enabled":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAutoFixture(manual("main", tc.action))
			rec := fx.run(t, "main", "req-"+tc.name, args)
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome, statuses(rec))
			assert.Equal(t, []string{tc.want}, fx.described(t))
		})
	}
}

func TestAutomationStreamControlLoginRequired(t *testing.T) {
	cases := map[string]obj{
		"raid":    {"login": "{arg:missing}"},
		"mod":     {"login": "{arg:missing}", "on": true},
		"vip":     {"login": "{arg:missing}", "on": true},
		"whisper": {"login": "{arg:missing}", "text": "x"},
	}
	for typ, params := range cases {
		t.Run(typ, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act(typ, params)))
			rec := fx.run(t, "main", "req-"+typ, nil)
			assert.Equal(t, automationrpc.OutcomeFailed, rec.Outcome)
			assert.Equal(t, typ+":failed:empty_login", statusDetail(rec, 0))
			assert.Empty(t, fx.pub.snapshot())
		})
	}
}

func statusDetail(rec automationrpc.RunRecord, i int) string {
	return rec.Actions[i].Type + ":" + rec.Actions[i].Status + ":" + rec.Actions[i].Detail
}

func TestProcessAutomationUnbanResolveUsesRequestID(t *testing.T) {
	cases := []struct {
		name   string
		params obj
		event  string
		want   []string
		run    string
	}{
		{"approve", obj{"status": "approve", "resolution": "welcome back"}, `{"id":"req-9"}`, []string{`unban_resolve {"request_id":"req-9","status":"approved","resolution":"welcome back"}`}, "t:ok"},
		{"deny", obj{"status": "deny"}, `{"id":"req-9"}`, []string{`unban_resolve {"request_id":"req-9","status":"denied"}`}, "t:ok"},
		{"no request", obj{"status": "deny"}, `{}`, nil, "t:failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			a := testAutomation("t", "event", map[string]any{"kind": "channel.unban_request.create"})
			a.Definition.Actions = []automation.Action{act("unban_resolve", tc.params)}
			p, runs := autoRig{automations: []automation.Automation{a}, pub: pub}.pipeline()
			process(t, p, eventEnv("channel.unban_request.create", tc.event))
			assert.Equal(t, []string{tc.run}, runs.summary())
			var got []string
			for _, c := range pub.snapshot() {
				got = append(got, describe(c.msg))
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestProcessAutomationModeratorActionsHonorPermCondition(t *testing.T) {
	a := command("!lock", map[string]any{}, cond("perm", map[string]any{"level": "lead_mod"}))
	a.Definition.Actions = []automation.Action{act("raid", obj{"login": "target"})}
	cases := []struct {
		name  string
		badge string
		want  string
	}{
		{"moderator below lead moderator", "moderator", "!lock:skipped:condition:0:perm"},
		{"regular chatter", "", "!lock:skipped:condition:0:perm"},
		{"broadcaster", "broadcaster", "!lock:ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, runs := autoRig{automations: []automation.Automation{a}}.pipeline()
			process(t, p, chatEnv("!lock", tc.badge))
			assert.Equal(t, []string{tc.want}, runs.summary())
		})
	}
}
