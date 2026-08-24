// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
)

// Moderation surfaces phrase-targeted mass moderation as a named KindDefault
// module (ships enabled; a broadcaster can disable it from the dashboard like
// any other module). It owns !nuke: a moderator sweeps the recent chat window
// for a phrase and everyone whose message matched is timed out within budget,
// with Shield Mode escalation on overflow when armed.
//
// The sweep memory and the escalation policy live engine-side (the pipeline
// records every chat line into Deps.Nuke's RecentLog); this module only
// registers the trigger. A nil Deps.Nuke leaves the command inert — the same
// graceful degradation every store-backed module here follows.
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

// nukeCooldown keeps a double-typed !nuke from firing two overlapping sweeps;
// a mod re-nuking mid-raid waits five seconds, which no real response needs
// to beat.
const nukeCooldown = 5 * time.Second
