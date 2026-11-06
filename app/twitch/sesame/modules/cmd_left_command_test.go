// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type leftCommandSource struct{ recipes []automation.Automation }

func (s leftCommandSource) Automations(context.Context, uint64) ([]automation.Automation, error) {
	return s.recipes, nil
}

func (s leftCommandSource) AutomationIndex(context.Context, uint64) (*automation.TriggerIndex, error) {
	return automation.CompileIndex(s.recipes), nil
}

func TestChatAddRefusesARecipeTimeLeftName(t *testing.T) {
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(`{"version":1,"trigger":{"type":"manual","params":{}},"conditions":[{"type":"cooldown","params":{"global_s":60,"left_command":"!timeleft"}}],"actions":[]}`, &def))
	cmds := &fakeCommandManager{}
	deps := cmdDeps(&fakeProj{commands: map[string]projection.Command{}}, cmds)
	deps.Automations.Source = leftCommandSource{recipes: []automation.Automation{{ID: "r", Name: "r", Enabled: true, Definition: def}}}

	out := runChat(t, Cmd(deps), chatCtx("42", "alice", "moderator"), "!cmd add timeleft Hello")

	assert.Empty(t, cmds.upserts)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Text, "already exists")
}
