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

func TestLightsHoldIsPlannedOutsideDefinitionBudgets(t *testing.T) {
	def := automation.Definition{
		Version: automation.Version,
		Trigger: automation.Trigger{Type: "manual", Params: codec.RawMessage(`{}`)},
		Actions: []automation.Action{
			{Type: "lights", Params: codec.RawMessage(`{"device":"AA:BB","sku":"H6008","color":"#f5a524","hold_s":60,"extra_lights":["CC:DD/H6159"]}`)},
			{Type: "delay", Params: codec.RawMessage(`{"ms":60000}`)},
		},
	}
	for len(def.Actions) < automation.MaxActions {
		def.Actions = append(def.Actions, automation.Action{Type: "chat", Params: codec.RawMessage(`{"text":"hello"}`)})
	}
	require.Empty(t, automation.Validate(def))
	plan := automation.NewPlan(automation.Automation{ID: "recipe", Definition: def})
	assert.Equal(t, automation.MaxActions, plan.StepCount)
	assert.Equal(t, [][]automation.Step{{
		{Type: "delay", Enabled: true, Internal: true, EffectKey: "auto:recipe:0.hold.0", Params: automation.DelayAction{Ms: 60000}},
		{Type: "lights_restore", Enabled: true, Internal: true, EffectKey: "auto:recipe:0.hold.1",
			Params: automation.LightsRestoreAction{Device: "AA:BB", SKU: "H6008", ExtraLights: []string{"CC:DD/H6159"}}},
	}}, plan.Steps[0].Branches)
}

func TestSavedLightsRestoreIsUnknown(t *testing.T) {
	def, issues := automation.Parse([]byte(`{"version":1,"trigger":{"type":"manual","params":{}},"actions":[{"type":"lights_restore","params":{"device":"AA:BB","sku":"H6008"}}]}`))
	assert.Equal(t, []automation.Issue{{Path: "actions[0].type", Code: automation.CodeUnknownType}}, issues)
	plan := automation.NewPlan(automation.Automation{ID: "recipe", Definition: def})
	assert.Nil(t, plan.Steps[0].Params)
}
