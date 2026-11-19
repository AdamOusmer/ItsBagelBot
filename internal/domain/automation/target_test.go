// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestTargetActionRegistry(t *testing.T) {
	cases := []struct {
		typ  string
		raw  string
		want any
	}{
		{"timeout", `{"who":"viewer","seconds":60}`, automation.TimeoutAction{
			TargetAction: automation.TargetAction{Who: "viewer"}, Seconds: 60,
		}},
		{"ban", `{"who":"mention"}`, automation.BanAction{
			TargetAction: automation.TargetAction{Who: "mention"},
		}},
		{"mod", `{"who":"user","user_id":"123","on":true}`, automation.LoginToggleAction{
			TargetAction: automation.TargetAction{Who: "user", UserID: "123"}, On: true,
		}},
		{"vip", `{"login":"bob"}`, automation.LoginToggleAction{
			TargetAction: automation.TargetAction{Who: "login", Login: "bob"},
		}},
		{"points_add", `{"who":"login","login":"{touser}","amount":10}`, automation.PointsAddAction{
			TargetAction: automation.TargetAction{Who: "login", Login: "{touser}"}, Amount: 10,
		}},
		{"points_set", `{"who":"user","user_id":"123","amount":20}`, automation.PointsSetAction{
			TargetAction: automation.TargetAction{Who: "user", UserID: "123"}, Amount: 20,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			def := automation.Definition{Actions: []automation.Action{{Type: tc.typ, Params: codec.RawMessage(tc.raw)}}}
			plan := automation.NewPlan(automation.Automation{Definition: def})
			require.Equal(t, tc.want, plan.Steps[0].Params)
		})
	}
}

func TestLegacyTargetDefinitionRoundTrip(t *testing.T) {
	raw := `{"version":1,"trigger":{"type":"command","params":{"name":"!points"}},"conditions":[],"actions":[
		{"type":"points_add","params":{"amount":10}},
		{"type":"points_set","params":{"login":"","amount":0}}
	]}`
	def, issues := automation.Parse([]byte(raw))
	require.Empty(t, issues)
	encoded, err := codec.Marshal(def)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(encoded))
}
