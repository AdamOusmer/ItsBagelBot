// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/stretchr/testify/require"
)

func TestAnalyticsModuleDefaultsMatchRuntime(t *testing.T) {
	mods := All(engine.Deps{Special: engine.NewSpecialSet("")})
	for _, mod := range mods {
		if !catalog.KnownModule(mod.Name) {
			continue
		}
		require.Equal(t, mod.Kind == module.KindDefault, catalog.ModuleEnabledByDefault(mod.Name), mod.Name)
	}
}
