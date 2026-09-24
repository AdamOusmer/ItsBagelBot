// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

func Cmd(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)

	m.Command("cmd").Aliases("cmds", "command", "commands").Everyone().Run(func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest := splitFirst(args)

		switch strings.ToLower(sub) {
		case "add", "edit", "remove", "delete":
			if !c.Chatter().Allows(module.RoleModerator) {
				cmdLink(ctx, c, d, emit)
				return nil
			}
			switch strings.ToLower(sub) {
			case "add":
				cmdAdd(ctx, c, d, rest, emit, log)
			case "edit":
				cmdEdit(ctx, c, d, rest, emit, log)
			default:
				cmdRemove(ctx, c, d, rest, emit, log)
			}
		default:
			cmdLink(ctx, c, d, emit)
		}
		return nil
	})

	m.Command("title").Aliases("settitle").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldTitle))
	m.Command("game").Aliases("setgame").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldGame))
	m.Command("tags").Aliases("settags").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldTags))
	m.Command("commercial").Aliases("ad").LeadMod().LiveOnly().Cooldown(streamCommercialCooldown).Run(streamCommercialRun(d))
	m.Command("marker").LeadMod().LiveOnly().Cooldown(streamMarkerCooldown).Run(streamMarkerRun(d))

	return m.Build()
}

func cmdAdd(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, response := splitFirst(args)
	if name == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
		return
	}
	if response == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.missing_resp"), c.Env.ChatterName(), "")
		return
	}

	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if _, found, _ := d.Proj.Command(ctx, c.BroadcasterID, name); found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.exists"), c.Env.ChatterName(), name)
		return
	}

	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, name, response); err != nil {
		log.Warn("cmd: add failed", zap.String("name", name), c.BID(), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.added"), c.Env.ChatterName(), name)
}

func cmdEdit(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, response := splitFirst(args)
	if name == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
		return
	}
	if response == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.missing_resp"), c.Env.ChatterName(), "")
		return
	}

	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if _, found, _ := d.Proj.Command(ctx, c.BroadcasterID, name); !found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.not_found"), c.Env.ChatterName(), name)
		return
	}

	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, name, response); err != nil {
		log.Warn("cmd: edit failed", zap.String("name", name), c.BID(), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.modified"), c.Env.ChatterName(), name)
}

func cmdRemove(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, _ := splitFirst(args)
	if name == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
		return
	}

	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if err := d.Commands.Delete(ctx, c.Env.BroadcasterUserID, name); err != nil {
		log.Warn("cmd: remove failed", zap.String("name", name), c.BID(), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.removed"), c.Env.ChatterName(), name)
}

func cmdLink(ctx context.Context, c *module.Context, d engine.Deps, emit module.Emit) {
	channel := c.Env.BroadcasterName()

	if u, err := d.Proj.User(ctx, c.BroadcasterID); err == nil && u.CommandsPageHidden {
		cmdPageOff(c, emit, channel)
		return
	}

	base := d.PublicBaseURL
	if base == "" {
		base = "https://commands.itsbagelbot.com"
	}
	slug := strings.ToLower(c.Env.BroadcasterUserLogin)
	if slug == "" {
		slug = c.Env.BroadcasterUserID
	}
	link := fmt.Sprintf("%s/user/%s", strings.TrimRight(base, "/"), slug)
	text := module.KV(
		"user", c.Env.ChatterName(),
		"channel", channel,
		"url", link,
	).WithLocale(module.Locale(c.Locale)).ExpandString(i18n.T(c.Locale, "cmd.link"))
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}

func cmdPageOff(c *module.Context, emit module.Emit, channel string) {
	text := module.KV(
		"user", c.Env.ChatterName(),
		"channel", channel,
	).WithLocale(module.Locale(c.Locale)).ExpandString(i18n.T(c.Locale, "cmd.page_off"))
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}

func reply(c *module.Context, emit module.Emit, line, user, command string) {
	text := module.KV("user", user, "command", command).WithLocale(module.Locale(c.Locale)).ExpandString(line)
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}

func splitFirst(s string) (first, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

const (
	streamFieldTitle = "title"
	streamFieldGame  = "game"
	streamFieldTags  = "tags"

	streamTitleMax       = 140
	streamTagMaxCount    = 10
	streamTagMaxLen      = 25
	streamCommercialMin  = 30
	streamCommercialMax  = 180
	streamCommercialStep = 30

	streamEditCooldown       = 5 * time.Second
	streamCommercialCooldown = 30 * time.Second
	streamMarkerCooldown     = 10 * time.Second
)

func streamFieldRun(d engine.Deps, field string) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !moduleEnabled(ctx, d, c.BroadcasterID, field) {
			return nil
		}
		value := strings.TrimSpace(args)
		if value == "" && streamIsSetAlias(c) {
			reply(c, emit, i18n.T(c.Locale, "stream."+field+".usage"), c.Env.ChatterName(), "")
			return nil
		}
		if key := streamFieldRefusal(field, value); key != "" {
			reply(c, emit, i18n.T(c.Locale, key), c.Env.ChatterName(), "")
			return nil
		}
		emitStreamUpdate(c, field, value, emit)
		return nil
	}
}

func streamFieldRefusal(field, value string) string {
	if value == "" {
		return ""
	}
	if field == streamFieldTitle && len([]rune(value)) > streamTitleMax {
		return "stream.title.too_long"
	}
	if field == streamFieldTags {
		if _, err := parseStreamTags(value); err != nil {
			return "stream.tags.usage"
		}
	}
	return ""
}

func streamCommercialRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !moduleEnabled(ctx, d, c.BroadcasterID, "commercial") {
			return nil
		}
		length, ok := parseCommercialLength(args)
		if !ok {
			reply(c, emit, i18n.T(c.Locale, "stream.commercial.usage"), c.Env.ChatterName(), "")
			return nil
		}
		emit(&module.Output{
			Type:          outgress.TypeCommercial,
			BroadcasterID: c.Env.BroadcasterUserID,
			Duration:      float64(length),
			Template:      c.Locale,
			To:            c.Env.ChatterName(),
		})
		return nil
	}
}

func streamMarkerRun(d engine.Deps) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if !moduleEnabled(ctx, d, c.BroadcasterID, "marker") {
			return nil
		}
		desc := strings.TrimSpace(args)
		if len([]rune(desc)) > streamTitleMax {
			desc = string([]rune(desc)[:streamTitleMax])
		}
		emit(&module.Output{
			Type:          outgress.TypeStreamMarker,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          desc,
			Template:      c.Locale,
			To:            c.Env.ChatterName(),
		})
		return nil
	}
}

func emitStreamUpdate(c *module.Context, field, value string, emit module.Emit) {
	emit(&module.Output{
		Type:          outgress.TypeChannelUpdate,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          value,
		Reason:        field,
		Template:      c.Locale,
		To:            c.Env.ChatterName(),
	})
}

func streamIsSetAlias(c *module.Context) bool {
	t := strings.TrimSpace(c.Env.Text)
	t = strings.TrimPrefix(t, "!")
	first, _, _ := strings.Cut(t, " ")
	switch strings.ToLower(first) {
	case "settitle", "setgame", "settags":
		return true
	}
	return false
}

func parseStreamTags(s string) ([]string, error) {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		tag := strings.TrimSpace(p)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > streamTagMaxLen {
			return nil, fmt.Errorf("tag too long")
		}
		out = append(out, tag)
	}
	if len(out) == 0 || len(out) > streamTagMaxCount {
		return nil, fmt.Errorf("tag count")
	}
	return out, nil
}

func parseCommercialLength(args string) (int, bool) {
	s := strings.TrimSpace(args)
	if s == "" {
		return streamCommercialMin, true
	}
	n, err := strconv.Atoi(strings.Fields(s)[0])
	if err != nil {
		return 0, false
	}
	if n < streamCommercialMin || n > streamCommercialMax {
		return 0, false
	}
	if n%streamCommercialStep != 0 {
		return 0, false
	}
	return n, true
}
