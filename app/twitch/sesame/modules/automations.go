// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
)

func Automations(d engine.Deps) module.Module {
	// Beta is mirrored by `beta: true` in web/kit/lib/catalog/automations.ts; flip both together.
	m := module.NewModule(engine.AutomationsModuleName, module.KindOptIn).Beta()
	d.AutomationHook.Register(m)
	return m.Build()
}
