// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func TestStreamCommandsRequireTheAppropriateRole(t *testing.T) {
	var policy struct {
		Actions []automation.Action
		Callers []struct {
			Level        string
			PermRequired bool
		}
	}
	require.NoError(t, codec.Unmarshal(readFile(t, "testdata/stream_action_roles.json"), &policy))
	for _, caller := range policy.Callers {
		perms, err := codec.Marshal(automation.PermCondition{Level: caller.Level})
		require.NoError(t, err)
		var want []automation.Issue
		if caller.PermRequired {
			want = []automation.Issue{{Path: "actions[0].type", Code: "perm_required"}}
		}
		for _, action := range policy.Actions {
			t.Run(caller.Level+"/"+action.Type, func(t *testing.T) {
				def := automation.Definition{
					Version:    1,
					Trigger:    automation.Trigger{Type: "command", Params: []byte(`{"name":"!run"}`)},
					Conditions: []automation.Condition{{Type: "perm", Params: perms}},
					Actions:    []automation.Action{action},
				}
				require.Equal(t, want, automation.Validate(def))
			})
		}
	}
}

func TestSelfTargetRoles(t *testing.T) {
	var cases []struct {
		Trigger          automation.Trigger
		Action           automation.Action
		WantPermRequired bool
	}
	require.NoError(t, codec.Unmarshal(readFile(t, "testdata/self_target_roles.json"), &cases))
	for _, tc := range cases {
		t.Run(tc.Action.Type+"/"+string(tc.Action.Params), func(t *testing.T) {
			var want []automation.Issue
			if tc.WantPermRequired {
				want = []automation.Issue{{Path: "actions[0].type", Code: "perm_required"}}
			}
			def := automation.Definition{
				Version: 1,
				Trigger: tc.Trigger,
				Actions: []automation.Action{tc.Action},
			}
			require.Equal(t, want, automation.Validate(def))
		})
	}
}

func TestUnbanResolutionCannotAttachToAnUnrelatedEvent(t *testing.T) {
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(readFile(t, "testdata/valid/unban_resolve_event.json"), &def))
	def.Trigger.Params = []byte(`{"kind":"channel.follow"}`)
	require.Equal(t, []automation.Issue{{Path: "actions[0].type", Code: "trigger_mismatch"}}, automation.Validate(def))
}
