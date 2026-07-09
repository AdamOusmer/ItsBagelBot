package modules

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// Cmd is the always-on commands module. It has two halves:
//
//   - A public link. Anyone can run !cmd / !cmds / !command / !commands (with no
//     subcommand) to get the channel's public command page.
//   - Moderator management. Mods add, edit and delete custom commands from chat:
//
//	!cmd add <name> <response>
//	!cmd edit <name> <response>
//	!cmd remove <name>
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
// "<base>/user/<broadcaster id>?channel=<display name>"; the path is keyed by the
// immutable broadcaster id, and the channel query is a display hint (the current
// display name) the page falls back to a numeric label without.
func cmdLink(c *module.Context, d engine.Deps, emit module.Emit) {
	base := d.PublicBaseURL
	if base == "" {
		base = "https://dashboard.itsbagelbot.com"
	}
	channel := c.Env.BroadcasterName()
	link := fmt.Sprintf("%s/user/%s", strings.TrimRight(base, "/"), c.Env.BroadcasterUserID)
	if channel != "" {
		link += "?channel=" + url.QueryEscape(channel)
	}
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
