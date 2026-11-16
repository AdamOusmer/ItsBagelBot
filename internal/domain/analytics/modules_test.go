// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"testing"

	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/modulevars"

	"github.com/stretchr/testify/require"
)

func TestKnownModulesMatchCatalog(t *testing.T) {
	want := []string{"moderation", "system"}
	for _, mod := range modulevars.Catalog() {
		want = append(want, mod.ID)
	}
	for _, name := range want {
		require.True(t, catalog.KnownModule(name), name)
	}
	require.ElementsMatch(t, want, catalog.ModuleNames())
}
