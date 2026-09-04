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

// Cmd is the always-on commands module. It has two halves:
//
//   - A public link. Anyone can run !cmd / !cmds / !command / !commands (with no
//     subcommand) to get the channel's public command page.
//
//   - Moderator management. Mods add, edit and delete custom commands from chat:
//
//     !cmd add <name> <response>
//     !cmd edit <name> <response>
//     !cmd remove <name>
//
//   - Stream editor. Lead moderators (and the broadcaster) set the live title,
//     category, tags, run a commercial, or drop a stream marker, matching the
//     Nightbot !title/!game/!tags/!commercial/!marker set plus StreamElements
//     !settitle/!setgame aliases. Each command is toggleable per broadcaster
//     (absent row = on) the same way !clip is, and they ship enabled.
//
// The command itself is open to everyone (so the link works for viewers); the
// mutating subcommands are gated on RoleModerator inside the handler. Mutations
// are forwarded to the commands service's dashboard RPC (via
// engine.CommandManager) so sesame stays read-only on the projection layer.
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
			// Managing commands stays moderator-only; a viewer who tries gets the
			// public link instead so the command is never a dead end for them.
			if !c.Chatter().Allows(module.RoleModerator) {
				cmdLink(c, d, emit)
				return nil
			}
			switch strings.ToLower(sub) {
			case "add":
				cmdAdd(ctx, c, d, rest, emit, log)
			case "edit":
				cmdEdit(ctx, c, d, rest, emit, log)
			default: // remove, delete
				cmdRemove(ctx, c, d, rest, emit, log)
			}
		default:
			// No (or unknown) subcommand: everyone gets the channel's page link.
			cmdLink(c, d, emit)
		}
		return nil
	})

	// Stream editor: LeadMod at the builder so the engine gate matches the
	// dashboard's defaultPerm. Each command is independently toggleable under
	// its trigger name (absent row = on). Commercial and marker are live-only
	// because Twitch rejects both on an offline channel.
	m.Command("title").Aliases("settitle").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldTitle))
	m.Command("game").Aliases("setgame").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldGame))
	m.Command("tags").Aliases("settags").LeadMod().Cooldown(streamEditCooldown).Run(streamFieldRun(d, streamFieldTags))
	m.Command("commercial").Aliases("ad").LeadMod().LiveOnly().Cooldown(streamCommercialCooldown).Run(streamCommercialRun(d))
	m.Command("marker").LeadMod().LiveOnly().Cooldown(streamMarkerCooldown).Run(streamMarkerRun(d))

	return m.Build()
}

// cmdAdd creates a new custom command. It checks for duplicates via the
// projection reader and forwards the mutation to the commands dashboard RPC.
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

	// Guard: reject if the command already exists.
	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if _, found, _ := d.Proj.Command(ctx, c.BroadcasterID, name); found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.exists"), c.Env.ChatterName(), name)
		return
	}

	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, name, response); err != nil {
		log.Warn("cmd: add failed", zap.String("name", name), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.added"), c.Env.ChatterName(), name)
}

// cmdEdit updates an existing custom command's response. It verifies the command
// exists before forwarding the mutation.
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

	// Guard: reject if the command does not exist.
	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if _, found, _ := d.Proj.Command(ctx, c.BroadcasterID, name); !found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.not_found"), c.Env.ChatterName(), name)
		return
	}

	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, name, response); err != nil {
		log.Warn("cmd: edit failed", zap.String("name", name), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.modified"), c.Env.ChatterName(), name)
}

// cmdRemove deletes a custom command.
func cmdRemove(ctx context.Context, c *module.Context, d engine.Deps, args string, emit module.Emit, log *zap.Logger) {
	name, _ := splitFirst(args)
	if name == "" {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
		return
	}

	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if err := d.Commands.Delete(ctx, c.Env.BroadcasterUserID, name); err != nil {
		log.Warn("cmd: remove failed", zap.String("name", name), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.removed"), c.Env.ChatterName(), name)
}

// cmdLink emits the channel's public command-page link. Any viewer can trigger
// it, so it is the everyone-facing half of the module. The URL is
// "<base>/user/<login>": the login is what a viewer reads in a shared link, and
// the page resolves it to the broadcaster id server-side before rendering
// anything.
//
// The link used to be "<base>/user/<id>?channel=<display name>", where the page
// took its channel label straight from that query string. Anyone could edit the
// query and hand out a link that showed one channel's commands under another
// streamer's name, so the name is no longer carried in the URL at all. The id
// stays the fallback path for links already shared in that older form.
func cmdLink(c *module.Context, d engine.Deps, emit module.Emit) {
	base := d.PublicBaseURL
	if base == "" {
		base = "https://commands.itsbagelbot.com"
	}
	channel := c.Env.BroadcasterName()
	slug := strings.ToLower(c.Env.BroadcasterUserLogin)
	if slug == "" {
		slug = c.Env.BroadcasterUserID
	}
	link := fmt.Sprintf("%s/user/%s", strings.TrimRight(base, "/"), slug)
	text := strings.NewReplacer(
		"{user}", c.Env.ChatterName(),
		"{channel}", channel,
		"{url}", link,
	).Replace(i18n.T(c.Locale, "cmd.link"))
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// reply emits a chat message with {user} and {command} variable expansion.
func reply(c *module.Context, emit module.Emit, tmpl, user, command string) {
	text := strings.NewReplacer("{user}", user, "{command}", command).Replace(tmpl)
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// splitFirst splits s on the first whitespace boundary, returning the first
// token and the rest (trimmed). If there is no whitespace, rest is empty.
func splitFirst(s string) (first, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// Stream-editor commands share one Helix Modify Channel Information call
// (title/game/tags) plus commercial and marker. Nightbot's !title/!game show
// the current value with no args and set it when args are present;
// StreamElements' !settitle/!setgame require args. We honour both: the
// set* aliases with no args print usage, the Nightbot spellings with no args
// ask outgress to read the live channel info.
const (
	streamFieldTitle = "title"
	streamFieldGame  = "game"
	streamFieldTags  = "tags"

	// Twitch's Modify Channel Information title cap. Sending more is a 400, so
	// we refuse here rather than paying Helix to say no.
	streamTitleMax = 140
	// Helix stream tags: at most 10, each 1–25 characters.
	streamTagMaxCount = 10
	streamTagMaxLen   = 25
	// Helix Start Commercial accepts these lengths only.
	streamCommercialMin  = 30
	streamCommercialMax  = 180
	streamCommercialStep = 30

	streamEditCooldown       = 5 * time.Second
	streamCommercialCooldown = 30 * time.Second
	streamMarkerCooldown     = 10 * time.Second
)

// streamFieldRun emits a TypeChannelUpdate for !title/!game/!tags (and their
// set* aliases). An empty argument on the Nightbot spelling is a get; the
// same empty argument on settitle/setgame/settags is usage. Over-long titles
// and illegal tag lists are refused here so outgress never sends a 400.
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

// streamFieldRefusal returns the i18n key refusing value for field, or ""
// when the value can be sent to outgress. An empty value is always sendable
// here — it is the Nightbot get spelling (the set-alias case is refused
// before validation ever runs).
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

// streamIsSetAlias reports whether the chatter typed a set* alias (settitle,
// setgame, settags) rather than the Nightbot show-or-set spelling. The engine
// resolves aliases onto the same command, so the typed trigger has to be
// recovered from the original chat line.
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

// parseStreamTags splits a comma-separated tag list the way Nightbot's !tags
// does. Empty pieces are dropped; more than 10 tags or a tag over 25 runes
// is rejected so the PATCH never 400s.
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

// parseCommercialLength reads a Twitch commercial length from args. Bare
// !commercial (no number) is 30, the shortest Helix accepts. Any other value
// must be 30/60/90/120/150/180 — Nightbot's set, and the only lengths Twitch
// will run.
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
