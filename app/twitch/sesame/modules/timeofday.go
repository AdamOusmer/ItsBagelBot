// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/tmpl"
	"ItsBagelBot/pkg/tzname"

	"go.uber.org/zap"
)

const (
	timeModuleName = engine.TimeModuleName
	timeCooldown   = 15 * time.Second

	// defaultTimeTemplate is the built-in !time reply, used when the broadcaster
	// leaves the message blank. Mirrored as the catalog defaultMessage in
	// web/kit/lib/catalog/time.ts.
	defaultTimeTemplate = "It is currently {time} for the streamer."

	// defaultLookupTemplate is the built-in !time <place> reply, used when the
	// broadcaster leaves LookupMessage blank.
	defaultLookupTemplate = "It is currently {time} in {place}."
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
// the module's enable state, so this only renders. args carries the optional
// place lookup ("!time tokyo"); blank args is the original home-zone reply.
func timeRun(d engine.Deps) module.RunFunc {
	return func(_ context.Context, c *module.Context, args string, emit module.Emit) error {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          timeReply(moduleLog(d), c, time.Now(), args),
		})
		return nil
	}
}

// timeReply picks the home-zone reply or a place lookup: any non-blank,
// normalizable args routes to the lookup, which needs no broadcaster
// configuration at all, so it answers even on a channel that never set one.
func timeReply(log *zap.Logger, c *module.Context, now time.Time, args string) string {
	if place := tzname.Normalize(args); place != "" {
		return timeLookupReply(c, now, place)
	}
	return timeHomeReply(log, c, now)
}

// timeHomeReply renders bare !time: the template expanded with the
// broadcaster's local time, or a fixed notice when the timezone is unset or
// no longer loads.
func timeHomeReply(log *zap.Logger, c *module.Context, now time.Time) string {
	var cfg timeConfig
	_ = c.Decode(&cfg)
	if strings.TrimSpace(cfg.Timezone) == "" {
		return i18n.T(c.Locale, "time.unset")
	}
	loc, ok := cfg.Zone()
	if !ok {
		log.Warn("time: configured timezone failed to load",
			c.BID(), zap.String("timezone", cfg.Timezone))
		return i18n.T(c.Locale, "time.unavailable")
	}
	text := strings.TrimSpace(cfg.Message)
	if text == "" {
		text = defaultTimeTemplate
	}
	return expandTimeTemplate(text, timeRender{local: now.In(loc), format: cfg.Format, timezone: strings.TrimSpace(cfg.Timezone)}, c)
}

// timeLookupReply renders !time <place>: an unknown place echoes the
// normalized query back in time.unknown, a known one expands the lookup
// template (or the built-in default) in the place's own zone.
func timeLookupReply(c *module.Context, now time.Time, place string) string {
	m, ok := tzname.Resolve(place)
	if !ok {
		return unknownPlaceReply(c, place)
	}
	var cfg timeConfig
	_ = c.Decode(&cfg)
	text := strings.TrimSpace(cfg.LookupMessage)
	if text == "" {
		text = defaultLookupTemplate
	}
	return expandTimeTemplate(text, timeRender{local: now.In(m.Loc), format: cfg.Format, timezone: m.Zone, place: m.Label}, c)
}

// unknownPlaceReply fills time.unknown's {place} placeholder with the
// normalized query, so chat sees back what it typed even after case-folding
// and accent-stripping.
func unknownPlaceReply(c *module.Context, place string) string {
	return module.ExpandString(i18n.T(c.Locale, "time.unknown"), func(tok tmpl.Token) (string, bool) {
		if tok.Key() == "place" {
			return place, true
		}
		return tmpl.Dynamic(tok)
	})
}

// timeRender is one instant ready to print: the local time plus the strings
// the reply's variables read. It is what differs between the home reply and a
// place lookup (place is empty on the home path, which never carries {place}
// in practice but resolves harmlessly if it did).
type timeRender struct {
	local    time.Time
	format   string
	timezone string
	place    string
}

// expandTimeTemplate fills a reply template's tokens from one timeRender.
// Both the home reply and the place lookup share it.
func expandTimeTemplate(text string, r timeRender, c *module.Context) string {
	return module.ExpandString(text, func(tok tmpl.Token) (string, bool) {
		switch tok.Key() {
		case "time":
			return engine.FormatClock(r.local, r.format), true
		case "date":
			return r.local.Format("Monday, January 2"), true
		case "timezone":
			return r.timezone, true
		case "place":
			return r.place, true
		case "user":
			return strings.TrimPrefix(c.Env.ChatterName(), "@"), true
		default:
			return tmpl.Dynamic(tok)
		}
	})
}
