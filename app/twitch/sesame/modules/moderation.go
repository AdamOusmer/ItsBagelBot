// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
)

func Moderation(d engine.Deps) module.Module {
	m := module.NewModule("moderation", module.KindDefault)
	m.Command("nuke").Mod().Cooldown(nukeCooldown).Run(func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.Nuke == nil {
			return nil
		}
		return d.Nuke.Execute(ctx, c, args, emit)
	})
	return m.Build()
}

const nukeCooldown = 5 * time.Second
