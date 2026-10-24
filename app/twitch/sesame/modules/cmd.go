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

type (
	cmdArgs     string
	cmdReplyKey string
	cmdPermKey  string
)

type cmdCall struct {
	ctx  context.Context
	c    *module.Context
	d    engine.Deps
	emit module.Emit
	log  *zap.Logger
}

type cmdAction struct {
	permKey cmdPermKey
	run     func(cmdCall, cmdArgs)
}

var cmdActions = map[string]cmdAction{
	"add":     {permKey: "addPerm", run: cmdCall.add},
	"edit":    {permKey: "editPerm", run: cmdCall.edit},
	"remove":  {permKey: "removePerm", run: cmdCall.remove},
	"delete":  {permKey: "removePerm", run: cmdCall.remove},
	"enable":  {permKey: "enablePerm", run: cmdSetActive(true, "cmd.enabled")},
	"disable": {permKey: "disablePerm", run: cmdSetActive(false, "cmd.disabled")},
	"perm":    {permKey: "permPerm", run: cmdCall.perm},
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
		action.run(cmdCall{ctx: ctx, c: c, d: d, emit: emit, log: log}, cmdArgs(rest))
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

func cmdRole(c *module.Context, permKey cmdPermKey) module.Role {
	var cfg map[string]any
	_ = c.Decode(&cfg)
	perm, _ := cfg[string(permKey)].(string)
	if role, ok := cmdManageRoles[perm]; ok {
		return role
	}
	return module.RoleModerator
}

func (k cmdCall) add(args cmdArgs) {
	name, response := splitFirst(string(args))
	if !k.hasNameAndResponse(name, response) {
		return
	}

	name = cmdName(name)
	_, found, err := k.d.Proj.Command(k.ctx, k.c.BroadcasterID, name)
	if err != nil {
		k.log.Warn("cmd: add lookup failed", zap.String("name", name), k.c.BID(), zap.Error(err))
		return
	}
	if found {
		k.reply("cmd.err.exists", name)
		return
	}

	k.save(projection.Command{Name: name, Response: response, IsActive: true, Perm: "everyone"}, "cmd.added")
}

func (k cmdCall) edit(args cmdArgs) {
	name, response := splitFirst(string(args))
	if !k.hasNameAndResponse(name, response) {
		return
	}
	k.change(name, "cmd.modified", func(cc *projection.Command) { cc.Response = response })
}

func cmdSetActive(active bool, replyKey cmdReplyKey) func(cmdCall, cmdArgs) {
	return func(k cmdCall, args cmdArgs) {
		name, _ := splitFirst(string(args))
		if name == "" {
			reply(k.c, k.emit, i18n.T(k.c.Locale, "cmd.err.usage"), "", "")
			return
		}
		k.change(name, replyKey, func(cc *projection.Command) { cc.IsActive = active })
	}
}

func (k cmdCall) perm(args cmdArgs) {
	name, rest := splitFirst(string(args))
	perm, _ := splitFirst(rest)
	perm = strings.ToLower(perm)
	if name == "" || validate.Perm(perm) != nil {
		k.reply("cmd.err.perm_usage", "")
		return
	}
	k.change(name, "cmd.perm_set", func(cc *projection.Command) { cc.Perm = perm })
}

func (k cmdCall) remove(args cmdArgs) {
	name, _ := splitFirst(string(args))
	if name == "" {
		reply(k.c, k.emit, i18n.T(k.c.Locale, "cmd.err.usage"), "", "")
		return
	}

	name = cmdName(name)
	if err := k.d.Commands.Delete(k.ctx, k.c.Env.BroadcasterUserID, name); err != nil {
		k.log.Warn("cmd: remove failed", zap.String("name", name), k.c.BID(), zap.Error(err))
		return
	}
	k.reply("cmd.removed", name)
}

func cmdName(name string) string {
	return strings.TrimPrefix(strings.ToLower(name), "!")
}

func (k cmdCall) reply(key cmdReplyKey, command string) {
	reply(k.c, k.emit, i18n.T(k.c.Locale, string(key)), k.c.Env.ChatterName(), command)
}

func (k cmdCall) hasNameAndResponse(name, response string) bool {
	if name == "" {
		reply(k.c, k.emit, i18n.T(k.c.Locale, "cmd.err.usage"), "", "")
		return false
	}
	if response == "" {
		k.reply("cmd.err.missing_resp", "")
		return false
	}
	return true
}

func (k cmdCall) change(name string, replyKey cmdReplyKey, apply func(*projection.Command)) {
	name = cmdName(name)
	cc, found, err := k.d.Proj.Command(k.ctx, k.c.BroadcasterID, name)
	if err != nil {
		k.log.Warn("cmd: lookup failed", zap.String("name", name), k.c.BID(), zap.Error(err))
		return
	}
	if !found {
		k.reply("cmd.err.not_found", name)
		return
	}
	apply(&cc)
	k.save(cc, replyKey)
}

func (k cmdCall) save(cc projection.Command, replyKey cmdReplyKey) {
	if err := k.d.Commands.Upsert(k.ctx, k.c.Env.BroadcasterUserID, cc); err != nil {
		k.log.Warn("cmd: save failed", zap.String("name", cc.Name), k.c.BID(), zap.Error(err))
		return
	}
	sendLine(k.c, k.emit, cmdReplyLine{text: i18n.T(k.c.Locale, string(replyKey)), user: k.c.Env.ChatterName(), command: cc.Name, role: cc.Perm})
}

func cmdLink(ctx context.Context, c *module.Context, d engine.Deps, emit module.Emit) {
	channel := c.Env.BroadcasterName()

	link, visible := publicPageURL(ctx, c, d, publicPage{
		base:        d.PublicBaseURL,
		defaultBase: "https://commands.itsbagelbot.com",
		path:        "/user/",
		hidden:      func(u projection.User) bool { return u.CommandsPageHidden },
	})
	if !visible {
		cmdPageOff(c, emit, channel)
		return
	}

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

type publicPage struct {
	base, defaultBase, path string
	hidden                  func(projection.User) bool
}

func publicPageURL(ctx context.Context, c *module.Context, d engine.Deps, page publicPage) (string, bool) {
	if d.Proj != nil {
		if u, err := d.Proj.User(ctx, c.BroadcasterID); err == nil && page.hidden(u) {
			return "", false
		}
	}
	base := page.base
	if base == "" {
		base = page.defaultBase
	}
	slug := strings.ToLower(c.Env.BroadcasterUserLogin)
	if slug == "" {
		slug = c.Env.BroadcasterUserID
	}
	return strings.TrimRight(base, "/") + page.path + slug, true
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
	switch c.Command {
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
