// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
)

func Automod(_ engine.Deps) module.Module {
	// Beta is mirrored by `beta: true` in web/kit/lib/catalog/automod.ts; flip both together.
	m := module.NewModule("automod", module.KindDefault).Beta()
	m.On("channel.chat.message", func(context.Context, *module.Context, module.Emit) error {
		return nil
	})
	return m.Build()
}
