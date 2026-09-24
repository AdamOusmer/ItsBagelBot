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
	"ItsBagelBot/pkg/tzname"

	"go.uber.org/zap"
)

const (
	timeModuleName = engine.TimeModuleName
	timeCooldown   = 15 * time.Second
)

type timeConfig = engine.TimeModuleConfig

func TimeOfDay(d engine.Deps) module.Module {
	m := module.NewModule(timeModuleName, module.KindOptIn)
	m.Command("time").Everyone().Cooldown(timeCooldown).Run(timeRun(d))
	return m.Build()
}

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

func timeReply(log *zap.Logger, c *module.Context, now time.Time, args string) string {
	if place := tzname.Normalize(args); place != "" {
		return timeLookupReply(c, now, place)
	}
	return timeHomeReply(log, c, now)
}

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
		text = i18n.T(c.Locale, "time.default")
	}
	return expandTimeTemplate(text, timeRender{local: now.In(loc), format: cfg.Format, timezone: strings.TrimSpace(cfg.Timezone)}, c)
}

func timeLookupReply(c *module.Context, now time.Time, place string) string {
	m, ok := tzname.Resolve(place)
	if !ok {
		return unknownPlaceReply(c, place)
	}
	var cfg timeConfig
	_ = c.Decode(&cfg)
	text := strings.TrimSpace(cfg.LookupMessage)
	if text == "" {
		text = i18n.T(c.Locale, "time.lookup")
	}
	return expandTimeTemplate(text, timeRender{local: now.In(m.Loc), format: cfg.Format, timezone: m.Zone, place: m.Label}, c)
}

func unknownPlaceReply(c *module.Context, place string) string {
	return module.KV("place", place).WithLocale(module.Locale(c.Locale)).ExpandString(i18n.T(c.Locale, "time.unknown"))
}

type timeRender struct {
	local    time.Time
	format   string
	timezone string
	place    string
}

var timeReplySpec = module.Spec{Entries: []module.SpecEntry{
	{Name: "time", Doc: "the local clock time, formatted per the module's Format setting"},
	{Name: "date", Doc: "the local date, e.g. \"Monday, January 2\""},
	{Name: "timezone", Doc: "the resolved timezone name or offset"},
	{Name: "place", Doc: "the looked-up place name (place lookups only)"},
	{Name: "user", Doc: "the invoking chatter's display name"},
}}

func expandTimeTemplate(text string, r timeRender, c *module.Context) string {
	p := timeReplySpec.Bind(func(name string) func() string {
		switch name {
		case "time":
			return func() string { return engine.FormatClock(r.local, r.format) }
		case "date":
			return func() string { return r.local.Format("Monday, January 2") }
		case "timezone":
			return func() string { return r.timezone }
		case "place":
			return func() string { return r.place }
		case "user":
			return func() string { return strings.TrimPrefix(c.Env.ChatterName(), "@") }
		default:
			return func() string { return "" }
		}
	}).WithLocale(module.Locale(c.Locale))
	return p.ExpandString(text)
}
