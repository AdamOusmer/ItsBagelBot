// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/pkg/codec"

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
		{"timeout", act("timeout", obj{"login": "@Target", "seconds": 60, "reason": "spam {args}"}), []string{`timeout {"data":{"user_id":"99","duration":60,"reason":"spam hello v"}}`}},
		{"ban", act("ban", obj{"login": "target", "reason": "bye"}), []string{`ban {"data":{"user_id":"99","reason":"bye"}}`}},
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
		})
	}
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

func TestAutomationChatMessageCap(t *testing.T) {
	var actions []automation.Action
	for range 4 {
		actions = append(actions, act("chat", obj{"text": "line"}))
	}
	fx := newAutoFixture(manual("main", actions...))
	rec := fx.run(t, "main", "req-cap", nil)
	assert.Equal(t, []string{"0:chat:ok:", "1:chat:ok:", "2:chat:ok:", "3:chat:limited:chat_cap"}, statuses(rec))
	assert.Equal(t, automationrpc.OutcomePartial, rec.Outcome)
	assert.Len(t, fx.messages(t), automation.MaxChatMessagesPerRun)
}
