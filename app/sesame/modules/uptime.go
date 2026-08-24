// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
)

const (
	uptimeModuleName = "uptime"
	uptimeCooldown   = 15 * time.Second
)

// Uptime owns !uptime: how long the broadcaster's current stream has been
// running. The live flag and the session start come from one cached lookup
// through outgress (Helix Get Streams), so the reply can never pair "live"
// with a stale clock. Toggleable per broadcaster under its own module key,
// checked lazily on use.
func Uptime(d engine.Deps) module.Module {
	log := moduleLog(d)

	m := module.NewModule("", module.KindCore)
	m.Command("uptime").Everyone().Cooldown(uptimeCooldown).Run(func(ctx context.Context, c *module.Context, _ string, emit module.Emit) error {
		if !moduleEnabled(ctx, d, c.BroadcasterID, uptimeModuleName) {
			return nil
		}
		bid := c.Env.BroadcasterUserID
		text := lookupCall[engine.UptimeResult]{
			log: log, logKey: uptimeModuleName, unavailable: i18n.T(c.Locale, "uptime.unavailable"),
			read: readIf(d.Uptime != nil, func() (engine.UptimeResult, error) { return d.Uptime.Lookup(ctx, bid) }),
			format: func(res engine.UptimeResult) string {
				if !res.Live {
					return i18n.T(c.Locale, "uptime.offline")
				}
				return fmt.Sprintf(i18n.T(c.Locale, "uptime"), humanizeDuration(c.Locale, time.Since(res.StartedAt)))
			},
		}.run()
		emitLookup(c, text, emit)
		return nil
	})
	return m.Build()
}
