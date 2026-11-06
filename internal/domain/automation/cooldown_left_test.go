// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leftRecipe(t *testing.T, raw string, enabled bool) automation.Automation {
	t.Helper()
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(raw, &def))
	require.Empty(t, automation.Validate(def))
	return automation.Automation{ID: "r", Name: "r", Enabled: enabled, Definition: def}
}

const nestedLeft = `{"version":1,"trigger":{"type":"reward","params":{"reward_id":"11111111-2222-3333-4444-555555555555"}},
"conditions":[{"type":"cooldown","params":{"user_s":60,"left_command":"!top"}}],
"actions":[{"type":"if","params":{"condition":{"type":"cooldown","params":{"global_s":60,"left_command":"!nested"}},"then":[
  {"type":"if","enabled":false,"params":{"condition":{"type":"cooldown","params":{"global_s":60,"left_command":"!off"}},"then":[],"else":[]}}
],"else":[{"type":"chat","params":{"text":"wait {cooldown:left}"}}]}}]}`

func TestLeftCommandsResolveThroughTheIndexWhateverTheTrigger(t *testing.T) {
	index := automation.CompileIndex([]automation.Automation{leftRecipe(t, nestedLeft, true)})

	for _, name := range []string{"top", "NESTED"} {
		found := index.Command(name)
		require.Len(t, found, 1, name)
		assert.NotNil(t, found[0].Left, name)
	}
	assert.Empty(t, index.Command("off"), "a turned off step answers nothing")
	assert.True(t, index.Plan("r").ShowsCooldownLeft)
}

func TestLeftCommandsOfADisabledRecipeAreSilent(t *testing.T) {
	index := automation.CompileIndex([]automation.Automation{leftRecipe(t, nestedLeft, false)})

	assert.Empty(t, index.Command("top"))
}

func TestLeftCommandWithoutABangGoesThroughTheChatScan(t *testing.T) {
	raw := `{"version":1,"trigger":{"type":"manual","params":{}},"conditions":[{"type":"cooldown","params":{"global_s":60,"left_command":"timeleft"}}],"actions":[]}`
	index := automation.CompileIndex([]automation.Automation{leftRecipe(t, raw, true)})

	scan := index.ChatScan()
	require.Len(t, scan, 1)
	text := automation.NewMatchText("TIMELEFT please")
	_, matched := scan[0].MatchChat(&text)
	assert.True(t, matched)
	assert.NotNil(t, scan[0].Left)
}

func TestLeftCommandNamesCollectEveryNode(t *testing.T) {
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(nestedLeft, &def))

	assert.Equal(t, []string{"!top", "!nested", "!off"}, automation.LeftCommandNames(def))
}
