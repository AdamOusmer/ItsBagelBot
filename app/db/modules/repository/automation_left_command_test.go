// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"testing"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/rpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leftCommandDef(trigger, left string) []byte {
	return []byte(fmt.Sprintf(`{"version":1,"trigger":{"type":"command","params":{"name":%q}},"conditions":[{"type":"cooldown","params":{"global_s":60,"left_command":%q}}],"actions":[{"type":"chat","params":{"text":"hi"}}]}`, trigger, left))
}

func TestLeftCommandClashesAreRefusedLikeACommandName(t *testing.T) {
	_, _, repo := setupAutomations(t, "modautomationleft")
	ctx := t.Context()
	saved, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: "first", Definition: leftCommandDef("!first", "!left")})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		definition []byte
		path       string
	}{
		"another recipe's trigger":      {leftCommandDef("!second", "!first"), "left_command"},
		"another recipe's left command": {leftCommandDef("!second", "LEFT"), "left_command"},
		"a trigger on a left command":   {commandDef("!left"), "trigger.params.name"},
	} {
		_, err := repo.Upsert(ctx, 1001, repository.AutomationInput{Name: name, Definition: tc.definition})

		refused := refusalOf(t, err)
		assert.Equal(t, rpc.CodeConflict, refused.Code, name)
		assert.Equal(t, []automation.Issue{{Path: tc.path, Code: automation.CodeCommandExists}}, refused.Issues, name)
	}

	rev := saved.Revision
	_, err = repo.Upsert(ctx, 1001, repository.AutomationInput{ID: saved.ID, Name: "first", Definition: leftCommandDef("!first", "!left"), ExpectedRevision: &rev})
	assert.NoError(t, err, "a recipe does not clash with itself")
	_, err = repo.Upsert(ctx, 1002, repository.AutomationInput{Name: "first", Definition: leftCommandDef("!first", "!left")})
	assert.NoError(t, err, "another channel keeps its own names")
}
