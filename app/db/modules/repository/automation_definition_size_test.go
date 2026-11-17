// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strings"
	"testing"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutomationDefinitionByteBoundary(t *testing.T) {
	_, _, repo := setupAutomations(t, "automation-definition-size")
	message := strings.Repeat("x", 100)
	chat := `{"type":"chat","params":{"text":"` + message + `"}}`
	actions := strings.TrimSuffix(strings.Repeat(chat+",", automation.MaxActions), ",")
	definition := `{"version":1,"trigger":{"type":"manual"},"conditions":null,"actions":[` + actions + `]}`
	cases := []struct {
		name  string
		extra int
	}{
		{name: "at"},
		{name: "above", extra: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := definition + strings.Repeat(" ", automation.MaxDefinitionBytes-len(definition)+tc.extra)
			saved, err := repo.Upsert(t.Context(), 7, repository.AutomationInput{Name: tc.name, Definition: []byte(raw)})
			if tc.extra > 0 {
				assert.Contains(t, refusalOf(t, err).Issues, automation.Issue{Path: "definition", Code: automation.CodeTooLong})
				return
			}
			require.NoError(t, err)
			loaded, found, err := repo.Get(t.Context(), 7, saved.ID)
			require.NoError(t, err)
			require.True(t, found)
			body, err := codec.Marshal(loaded.Definition)
			require.NoError(t, err)
			assert.JSONEq(t, definition, string(body))
		})
	}
}
