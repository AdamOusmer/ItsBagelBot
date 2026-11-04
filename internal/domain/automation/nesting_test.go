// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"fmt"
	"strings"
	"testing"

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
	branch := fmt.Sprintf(`{"type":"if","params":{"condition":%s}}`, liveCondition)
	list := strings.TrimSuffix(strings.Repeat(branch+",", MaxActions+5), ",")
	raw := fmt.Sprintf(`{"version":1,"trigger":{"type":"manual","params":{}},"actions":[%s]}`, list)
	_, issues := Parse([]byte(raw))
	assert.Empty(t, issues)
}
