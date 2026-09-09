// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	timeModuleName = engine.TimeModuleName
	timeCooldown   = 15 * time.Second

	// defaultTimeTemplate is the built-in !time reply, used when the broadcaster
	// leaves the message blank. Mirrored as the catalog defaultMessage in
	// console/shared/lib/types.ts.
	defaultTimeTemplate = "It is currently {time} for the streamer."

	// timeUnsetReply answers !time on an enabled but unconfigured module, telling
	// chat (and the broadcaster) what is missing instead of staying silent.
	timeUnsetReply = "The streamer hasn't set their timezone yet."
)

// timeConfig is the module's dashboard configuration. It lives in the engine
// (engine.TimeModuleConfig) because the {time} response token decodes the same
// blob and the engine cannot import this package; the alias keeps every use
// here reading as the module's own type.
type timeConfig = engine.TimeModuleConfig

// TimeOfDay owns !time: it answers with the broadcaster's current local time in
// their configured timezone. It is a named, opt-in module (KindOptIn): off by
// default, enabled and configured on its dashboard module page.
func TimeOfDay(d engine.Deps) module.Module {
	m := module.NewModule(timeModuleName, module.KindOptIn)
	m.Command("time").Everyone().Cooldown(timeCooldown).Run(timeRun(d))
	return m.Build()
}

// timeRun emits the !time reply. The engine has already gated the command on
// the module's enable state, so this only renders.
func timeRun(d engine.Deps) module.RunFunc {
	return func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          timeReply(moduleLog(d), c, time.Now()),
		})
		return nil
	}
}

// timeReply renders the reply for one !time at the given instant: the template
// expanded with the broadcaster's local time, or a fixed notice when the
// timezone is unset or no longer loads.
func timeReply(log *zap.Logger, c *module.Context, now time.Time) string {
	var cfg timeConfig
	_ = c.Decode(&cfg)
	if strings.TrimSpace(cfg.Timezone) == "" {
		return timeUnsetReply
	}
	loc, ok := cfg.Zone()
	if !ok {
		log.Warn("time: configured timezone failed to load",
			c.BID(), zap.String("timezone", cfg.Timezone))
		return "The time is unavailable right now."
	}
	return expandTimeTemplate(cfg, now.In(loc), c)
}

// expandTimeTemplate fills the reply template's tokens for the broadcaster's
// local instant, falling back to defaultTimeTemplate on a blank template.
func expandTimeTemplate(cfg timeConfig, local time.Time, c *module.Context) string {
	tmpl := strings.TrimSpace(cfg.Message)
	if tmpl == "" {
		tmpl = defaultTimeTemplate
	}
	return module.ExpandString(tmpl, func(key string) (string, bool) {
		switch key {
		case "time":
			return engine.FormatClock(local, cfg.Format), true
		case "date":
			return local.Format("Monday, January 2"), true
		case "timezone":
			return strings.TrimSpace(cfg.Timezone), true
		case "user":
			return strings.TrimPrefix(c.Env.ChatterName(), "@"), true
		default:
			return module.ParseDynamic(key)
		}
	})
}
