// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"maps"
	"slices"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationEveryActionTypeMatchesDomainParams(t *testing.T) {
	for _, typ := range slices.Sorted(maps.Keys(actionTable)) {
		t.Run(typ, func(t *testing.T) {
			fx := newAutoFixture(manual("main", act(typ, obj{})))
			rec := fx.run(t, "main", "req-"+typ, nil)
			for _, a := range rec.Actions {
				assert.NotEqual(t, "bad_params", a.Detail, statuses(rec))
			}
		})
	}
}

func TestAutomationEveryConditionTypeMatchesDomainParams(t *testing.T) {
	for _, typ := range slices.Sorted(maps.Keys(conditionChecks)) {
		t.Run(typ, func(t *testing.T) {
			auto := manual("main", act("stop", obj{}))
			auto.Definition.Conditions = []automation.Condition{{Type: typ, Params: codec.RawMessage(`{}`)}}
			fx := newAutoFixture(auto)
			rec := fx.run(t, "main", "req-"+typ, nil)
			assert.NotEqual(t, errConditionParams.Error(), rec.Reason)
		})
	}
}

func expandedChat(t *testing.T, template, text string) []string {
	t.Helper()
	pub := &fakePublisher{}
	rig := autoRig{pub: pub, automations: []automation.Automation{{ID: "t", UserID: 123, Name: "t", Enabled: true, Revision: 1, Definition: automation.Definition{Version: 1,
		Trigger: automation.Trigger{Type: "command", Params: rawJSON(obj{"name": "!hi"})},
		Actions: []automation.Action{act("chat", obj{"text": template})},
	}}}}
	p, _ := rig.pipeline()
	process(t, p, lane.Envelope{Type: chatType, BroadcasterUserID: "123", BroadcasterUserLogin: "chan", BroadcasterUserName: "Chan", ChatterUserID: "999", ChatterUserLogin: "viewer", ChatterUserName: "Viewer", Text: text, MsgID: "m1"})
	var out []string
	for _, c := range pub.snapshot() {
		for _, m := range flattenMessage(t, c.msg) {
			out = append(out, describe(m))
		}
	}
	return out
}

func TestAutomationTemplateExpansion(t *testing.T) {
	cases := []struct{ template, want string }{
		{"plain", "plain"},
		{"{", "{"},
		{"}", "}"},
		{"{}", "{}"},
		{"{args}", "Bob two {user}"},
		{"{args|fb}", "Bob two {user}"},
		{"{args:x}", "{args:x}"},
		{"{args} {count:y}", "Bob two {user} {count:y}"},
		{"{1}", "Bob"},
		{"{1:2}", "Bob two"},
		{"{2:}", "two {user}"},
		{"{touser}", "Bob"},
		{"{user}", "Viewer"},
		{"{USER}", "Viewer"},
		{"{user|fb}", "Viewer"},
		{"{user} {args} {1} {touser}", "Viewer Bob two {user} Bob Bob"},
		{"{userid}", "999"},
		{"{channel}", "Chan"},
		{"{command}", "t"},
		{"{math:1+1}", "2"},
		{"{arg:nope|fb}", "fb"},
		{"{event.x|fb}", "fb"},
		{"{if:args:y:n}", "y"},
		{"{if:user=Viewer:y:n}", "y"},
		{"{unknown}", "{unknown}"},
		{"{unknown|fb}", "{unknown|fb}"},
		{"{upper:abc}", "{upper:abc}"},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			want, err := codec.Marshal(tc.want)
			require.NoError(t, err)
			assert.Equal(t, []string{`chat {"broadcaster_id":"123","message":` + string(want) + `}`},
				expandedChat(t, tc.template, "!hi Bob two {user}"))
		})
	}
}

func TestAutomationTemplateKeepsViewerBracesLiteral(t *testing.T) {
	got := expandedChat(t, "{args}", "!hi {user}")
	assert.Equal(t, []string{`chat {"broadcaster_id":"123","message":"{user}"}`}, got)
}
