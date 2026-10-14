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
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

const cmdModuleName = "cmd"

type cmdHandler func(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger)

type cmdAction struct {
	permKey string
	run     cmdHandler
}

var cmdActions = map[string]cmdAction{
	"add":     {permKey: "addPerm", run: cmdAdd},
	"edit":    {permKey: "editPerm", run: cmdEdit},
	"remove":  {permKey: "removePerm", run: cmdRemove},
	"delete":  {permKey: "removePerm", run: cmdRemove},
	"enable":  {permKey: "enablePerm", run: cmdSetActive(true, "cmd.enabled")},
	"disable": {permKey: "disablePerm", run: cmdSetActive(false, "cmd.disabled")},
	"perm":    {permKey: "permPerm", run: cmdPerm},
}

func Cmd(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule(cmdModuleName, module.KindDefault)
	m.Command("cmd").Aliases("cmds", "command", "commands").Everyone().Run(cmdRun(d, log))
	return m.Build()
}

func StreamEditors(d engine.Deps) module.Module {
	m := module.NewModule("", module.KindCore)
	m.Command("title").Aliases("settitle").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldTitle))
	m.Command("game").Aliases("setgame").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldGame))
	m.Command("tags").Aliases("settags").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldTags))
	m.Command("commercial").Aliases("ad").LeadMod().LiveOnly().Cooldown(streamCommercialCooldown).Run(streamCommercialRun(d))
	m.Command("marker").LeadMod().LiveOnly().Cooldown(streamMarkerCooldown).Run(streamMarkerRun(d))
	return m.Build()
}

func cmdRun(d engine.Deps, log *zap.Logger) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest := splitFirst(args)
		action, ok := cmdActions[strings.ToLower(sub)]
		if !ok || !c.Chatter().Allows(cmdRole(c, action.permKey)) {
			cmdLink(ctx, c, d, emit)
			return nil
		}
		action.run(ctx, c, d, rest, emit, log)
		return nil
	}
}

var cmdManageRoles = map[string]module.Role{
	"sub":         module.RoleSubscriber,
	"vip":         module.RoleVIP,
	"mod":         module.RoleModerator,
	"lead_mod":    module.RoleLeadModerator,
	"broadcaster": module.RoleBroadcaster,
}

func cmdRole(c *module.Context, permKey string) module.Role {
	var cfg map[string]any
	_ = c.Decode(&cfg)
	perm, _ := cfg[permKey].(string)
	if role, ok := cmdManageRoles[perm]; ok {
		return role
	}
	return module.RoleModerator
}

func cmdAdd(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, response := splitFirst(args)
	if !cmdHasNameAndResponse(c, emit, name, response) {
		return
	}

	name = cmdName(name)
	_, found, err := d.Proj.Command(ctx, c.BroadcasterID, name)
	if err != nil {
		log.Warn("cmd: add lookup failed", zap.String("name", name), c.BID(), zap.Error(err))
		return
	}
	if found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.exists"), c.Env.ChatterName(), name)
		return
	}

	cmdSave(ctx, c, d, projection.Command{Name: name, Response: response, IsActive: true, Perm: "everyone"}, "cmd.added", emit, log)
}

func cmdEdit(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, response := splitFirst(args)
	if !cmdHasNameAndResponse(c, emit, name, response) {
		return
	}
	cc, ok := cmdLookup(ctx, c, d, cmdName(name), emit, log)
	if !ok {
		return
	}
	cc.Response = response
	cmdSave(ctx, c, d, cc, "cmd.modified", emit, log)
}

func cmdSetActive(active bool, replyKey string) cmdHandler {
	return func(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
		name, _ := splitFirst(args)
		if name == "" {
			reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
			return
		}
		cc, ok := cmdLookup(ctx, c, d, cmdName(name), emit, log)
		if !ok {
			return
		}
		cc.IsActive = active
		cmdSave(ctx, c, d, cc, replyKey, emit, log)
	}
}

func cmdPerm(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, rest := splitFirst(args)
	perm, _ := splitFirst(rest)
	perm = strings.ToLower(perm)
	if name == "" || validate.Perm(perm) != nil {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.perm_usage"), c.Env.ChatterName(), "")
		return
	}
	cc, ok := cmdLookup(ctx, c, d, cmdName(name), emit, log)
	if !ok {
		return
	}
	cc.Perm = perm
	cmdSave(ctx, c, d, cc, "cmd.perm_set", emit, log)
}

func cmdRemove(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, _ := splitFirst(args)
	if name == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
		return
	}

	name = cmdName(name)
	if err := d.Commands.Delete(ctx, c.Env.BroadcasterUserID, name); err != nil {
		log.Warn("cmd: remove failed", zap.String("name", name), c.BID(), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.removed"), c.Env.ChatterName(), name)
}

func cmdName(name string) string {
	return strings.TrimPrefix(strings.ToLower(name), "!")
}

func cmdHasNameAndResponse(c *module.Context, emit module.Emit, name, response string) bool {
	if name == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
		return false
	}
	if response == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.missing_resp"), c.Env.ChatterName(), "")
		return false
	}
	return true
}

func cmdLookup(ctx context.Context, c *module.Context, d engine.Deps, name string, emit module.Emit, log *zap.Logger) (projection.Command, bool) {
	cc, found, err := d.Proj.Command(ctx, c.BroadcasterID, name)
	if err != nil {
		log.Warn("cmd: lookup failed", zap.String("name", name), c.BID(), zap.Error(err))
		return cc, false
	}
	if !found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.not_found"), c.Env.ChatterName(), name)
	}
	return cc, found
}

func cmdSave(ctx context.Context, c *module.Context, d engine.Deps, cc projection.Command, replyKey string, emit module.Emit, log *zap.Logger) {
	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, cc); err != nil {
		log.Warn("cmd: save failed", zap.String("name", cc.Name), c.BID(), zap.Error(err))
		return
	}
	sendLine(c, emit, cmdReplyLine{text: i18n.T(c.Locale, replyKey), user: c.Env.ChatterName(), command: cc.Name, role: cc.Perm})
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

type cmdReplyLine struct {
	namespace                 module.Namespace
	text, user, command, role string
}

func reply(c *module.Context, emit module.Emit, line, user, command string) {
	sendLine(c, emit, cmdReplyLine{text: line, user: user, command: command})
}

func sendLine(c *module.Context, emit module.Emit, line cmdReplyLine) {
	text := module.KV("user", line.user, "command", line.command, "role", line.role).WithLocale(module.Locale(c.Locale)).WithNamespace(line.namespace).ExpandString(line.text)
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
			sendLine(c, emit, cmdReplyLine{namespace: module.Namespace(field), text: i18n.T(c.Locale, key), user: c.Env.ChatterName()})
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
