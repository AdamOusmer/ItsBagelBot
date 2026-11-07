// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
)

const liveCondition = `{"type":"live","params":{"state":"live"}}`

func deepRecipe(levels int) string {
	action := `{"type":"chat","params":{"text":"deep"}}`
	for range levels {
		action = fmt.Sprintf(`{"type":"if","params":{"condition":%s,"then":[%s]}}`, liveCondition, action)
	}
	return fmt.Sprintf(`{"version":1,"trigger":{"type":"manual","params":{}},"actions":[%s]}`, action)
}

func TestDeepNestingIsValidUpToTheSizeLimit(t *testing.T) {
	_, issues := Parse([]byte(deepRecipe(200)))
	assert.Empty(t, issues)
	_, issues = Parse([]byte(deepRecipe(5000)))
	assert.Equal(t, []Issue{{Code: CodeTooLong}}, issues)
}

func TestManyConditionsInARowAreValid(t *testing.T) {
	conditions := strings.TrimSuffix(strings.Repeat(liveCondition+",", 100), ",")
	raw := fmt.Sprintf(`{"version":1,"trigger":{"type":"manual","params":{}},"conditions":[%s]}`, conditions)
	_, issues := Parse([]byte(raw))
	assert.Empty(t, issues)
}

func TestBranchingStepsDoNotCountAsActions(t *testing.T) {
	branch := Action{Type: "if", Params: codec.RawMessage(fmt.Sprintf(`{"condition":%s}`, liveCondition))}
	assert.Empty(t, Validate(manualDefinition(slices.Repeat([]Action{branch}, MaxActions+5))))
}

func manualDefinition(actions []Action) Definition {
	return Definition{Version: 1, Trigger: Trigger{Type: "manual", Params: codec.RawMessage(`{}`)}, Actions: actions}
}

func TestEffectStepsOverTheLimitAreRefused(t *testing.T) {
	stop := Action{Type: "stop", Params: codec.RawMessage(`{}`)}
	assert.Empty(t, Validate(manualDefinition(slices.Repeat([]Action{stop}, MaxActions))))
	tooMany := []Issue{{Path: "actions", Code: CodeTooMany}}
	assert.Equal(t, tooMany, Validate(manualDefinition(slices.Repeat([]Action{stop}, MaxActions+1))))

	body, err := codec.Marshal(LoopAction{Times: "1", Actions: slices.Repeat([]Action{stop}, MaxActions+1)})
	assert.NoError(t, err)
	assert.Equal(t, tooMany, Validate(manualDefinition([]Action{{Type: "loop", Params: body}})))
}

func TestFetchNodesCountAsEffectsAndLoopsDoNot(t *testing.T) {
	assert.False(t, IsBranching("http_get"))
	assert.False(t, IsBranching("http_post"))
	assert.True(t, IsBranching("loop"))
}
