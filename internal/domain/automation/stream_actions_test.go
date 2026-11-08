// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func streamCommand(t *testing.T, typ, role string) automation.Definition {
	t.Helper()
	var def automation.Definition
	require.NoError(t, codec.Unmarshal([]byte(`{"version":1,"trigger":{"type":"command","params":{"name":"!run"}},"conditions":[],"actions":[{"type":"`+typ+`","params":{"login":"bob"}}]}`), &def))
	if role != "" {
		def.Conditions = []automation.Condition{{Type: "perm", Params: []byte(`{"level":"` + role + `"}`)}}
	}
	return def
}

func TestStreamCommandsRequireTheAppropriateRole(t *testing.T) {
	for _, typ := range []string{"raid", "mod", "vip"} {
		t.Run(typ, func(t *testing.T) {
			for _, role := range []string{"", "mod"} {
				issues := automation.Validate(streamCommand(t, typ, role))
				require.Equal(t, []automation.Issue{{Path: "actions[0].type", Code: "perm_required"}}, issues)
			}
			require.Empty(t, automation.Validate(streamCommand(t, typ, "lead_mod")))
		})
	}
}

func TestUnbanResolutionCannotAttachToAnUnrelatedEvent(t *testing.T) {
	var def automation.Definition
	require.NoError(t, codec.Unmarshal(readFile(t, "testdata/valid/unban_resolve_event.json"), &def))
	def.Trigger.Params = []byte(`{"kind":"channel.follow"}`)
	require.Equal(t, []automation.Issue{{Path: "actions[0].type", Code: "trigger_mismatch"}}, automation.Validate(def))
}
