// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"

	"github.com/stretchr/testify/assert"
)

func TestDefinitionByteBoundary(t *testing.T) {
	const definition = `{"version":1,"trigger":{"type":"manual"}}`
	cases := []struct {
		name  string
		extra int
		want  []automation.Issue
	}{
		{name: "below", extra: -1},
		{name: "at"},
		{name: "above", extra: 1, want: []automation.Issue{{Code: automation.CodeTooLong}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := definition + strings.Repeat(" ", automation.MaxDefinitionBytes-len(definition)+tc.extra)
			_, issues := automation.Parse([]byte(raw))
			assert.Equal(t, tc.want, issues)
		})
	}
}
