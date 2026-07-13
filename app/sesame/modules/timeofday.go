package modules

import (
	"context"
	"strings"
	"time"

	// The sesame image is distroless/static: there is no /usr/share/zoneinfo on
	// disk, so the IANA database must ride the binary for LoadLocation to work.
	_ "time/tzdata"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	timeModuleName = "time"
	timeCooldown   = 15 * time.Second

	// defaultTimeTemplate is the built-in !time reply, used when the broadcaster
	// leaves the message blank. Mirrored as the catalog defaultMessage in
	// console/shared/lib/types.ts.
	defaultTimeTemplate = "It is currently {time} for the streamer."

	// timeUnsetReply answers !time on an enabled but unconfigured module, telling
	// chat (and the broadcaster) what is missing instead of staying silent.
	timeUnsetReply = "The streamer hasn't set their timezone yet."
)

// timeConfig is the module's dashboard configuration. Timezone is an IANA zone
// name ("America/Toronto") the dashboard suggests from the viewer's browser
// (Intl.DateTimeFormat, computed client-side only — nothing is stored until the
// broadcaster saves). Format selects the clock face: "24" for 15:04, anything
// else the 12-hour default. Message is the reply template.
type timeConfig struct {
	Timezone string `json:"timezone"`
	Format   string `json:"format"`
	Message  string `json:"message"`
}

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
	tz := strings.TrimSpace(cfg.Timezone)
	if tz == "" {
		return timeUnsetReply
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Warn("time: configured timezone failed to load",
			zap.Uint64("broadcaster_id", c.BroadcasterID), zap.String("timezone", tz), zap.Error(err))
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
			return formatClock(local, cfg.Format), true
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

// formatClock renders the local time on the configured clock face: "24" gives
// 15:04, anything else the 12-hour default (3:04 PM).
func formatClock(t time.Time, format string) string {
	if format == "24" {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}
