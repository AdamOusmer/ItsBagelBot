// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"math"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countdownDefinition(t *testing.T, raw string) automation.Definition {
	t.Helper()
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(raw, &def))
	return def
}

func TestCountdownNamesAndCommandsFollowNestedStarts(t *testing.T) {
	def := countdownDefinition(t, `{"version":1,"trigger":{"type":"manual"},"actions":[
		{"type":"countdown_start","params":{"name":"Subathon","seconds":60,"left_command":"!left"}},
		{"type":"parallel","params":{"branches":[{"actions":[{"type":"countdown_start","params":{"name":"break","seconds":120,"left_command":"!break"}}]},{"actions":[{"type":"countdown_start","enabled":false,"params":{"name":"off","seconds":10,"left_command":"!off"}}]}]}}
	]}`)
	require.Empty(t, automation.Validate(def))
	assert.Equal(t, []string{"subathon", "break", "off"}, automation.OwnedCountdownNames(def))
	assert.Equal(t, []string{"!left", "!break", "!off"}, automation.LeftCommandNames(def))
	a := automation.Automation{ID: "countdown", Enabled: true, Definition: def}
	index := automation.CompileIndex([]automation.Automation{a})
	for _, name := range []string{"left", "BREAK"} {
		found := index.Command(name)
		require.Len(t, found, 1)
		assert.NotNil(t, found[0].CountdownLeft)
		assert.Nil(t, found[0].Left)
	}
	assert.Empty(t, index.Command("off"))
	a.Enabled = false
	assert.Empty(t, automation.CompileIndex([]automation.Automation{a}).Command("left"))
}

func TestCountdownFinishIndexIsNamedAndIndependentOfCooldowns(t *testing.T) {
	index := automation.CompileIndex([]automation.Automation{
		compiled("finish", "finish", "countdown", `{"name":"Subathon"}`),
		compiled("other", "other", "countdown", `{"name":"break"}`),
		disabled(compiled("off", "off", "countdown", `{"name":"subathon"}`)),
		compiled("bad", "bad", "countdown", `{"name":"bad-name"}`),
	})
	assert.Equal(t, []string{"finish"}, ids(index.Countdown("SUBATHON")))
	assert.Equal(t, []string{"other"}, ids(index.Countdown("break")))
	assert.Empty(t, index.Countdown("bad-name"))
	var empty *automation.TriggerIndex
	assert.Empty(t, empty.Countdown("subathon"))
}

func TestCountdownValidationAndCommandConflicts(t *testing.T) {
	for _, tc := range []struct{ name, raw, code string }{
		{"bad name", `{"version":1,"trigger":{"type":"countdown","params":{"name":"bad-name"}},"actions":[]}`, automation.CodeSimpleWord},
		{"zero duration", `{"version":1,"trigger":{"type":"manual"},"actions":[{"type":"countdown_start","params":{"name":"break","seconds":0}}]}`, automation.CodeOutOfRange},
		{"long duration", `{"version":1,"trigger":{"type":"manual"},"actions":[{"type":"countdown_start","params":{"name":"break","seconds":2592001}}]}`, automation.CodeOutOfRange},
		{"rate needs amount", `{"version":1,"trigger":{"type":"manual"},"actions":[{"type":"countdown_add","params":{"name":"break","seconds":10,"per":100}}]}`, automation.CodeTriggerMismatch},
		{"start needs role", `{"version":1,"trigger":{"type":"command","params":{"name":"!start"}},"actions":[{"type":"countdown_start","params":{"name":"break","seconds":10}}]}`, automation.CodePermRequired},
		{"add needs role", `{"version":1,"trigger":{"type":"command","params":{"name":"!add"}},"actions":[{"type":"countdown_add","params":{"name":"break","seconds":10}}]}`, automation.CodePermRequired},
		{"left command clashes with trigger", `{"version":1,"trigger":{"type":"command","params":{"name":"!left"}},"conditions":[{"type":"perm","params":{"level":"mod"}}],"actions":[{"type":"countdown_start","params":{"name":"break","seconds":10,"left_command":"LEFT"}}]}`, automation.CodeCommandExists},
		{"left command clashes with cooldown", `{"version":1,"trigger":{"type":"manual"},"conditions":[{"type":"cooldown","params":{"global_s":10,"left_command":"!left"}}],"actions":[{"type":"countdown_start","params":{"name":"break","seconds":10,"left_command":"!LEFT"}}]}`, automation.CodeCommandExists},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := automation.Validate(countdownDefinition(t, tc.raw))
			codes := make([]string, len(issues))
			for i, issue := range issues {
				codes[i] = issue.Code
			}
			assert.Contains(t, codes, tc.code)
		})
	}
}

func TestCountdownAdditionCountsFullAmountsAndBoundsOverflow(t *testing.T) {
	seconds, ok := automation.CountdownAddSeconds(automation.CountdownAddAction{Seconds: 30, Per: 100}, 250)
	assert.True(t, ok)
	assert.EqualValues(t, 60, seconds)
	_, ok = automation.CountdownAddSeconds(automation.CountdownAddAction{Seconds: 30, Per: 100}, 99)
	assert.False(t, ok)
	seconds, ok = automation.CountdownAddSeconds(automation.CountdownAddAction{Seconds: 30, Per: 1}, math.MaxInt64)
	assert.True(t, ok)
	assert.EqualValues(t, automation.MaxCooldownSeconds, seconds)
}

func TestCountdownTimeLeftTokenDoesNotMatchDateCountdown(t *testing.T) {
	assert.True(t, automation.IsCountdownLeftToken(tmpl.Lex("{countdown:left}")[0]))
	assert.False(t, automation.IsCountdownLeftToken(tmpl.Lex("{countdown:2026-12-25}")[0]))
	assert.False(t, automation.IsCountdownLeftToken(tmpl.Lex("{cooldown:left}")[0]))
}
