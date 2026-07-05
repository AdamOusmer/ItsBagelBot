package modules

import (
	"context"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/i18n"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// Cmd is the always-on command-management module. It gives moderators the
// ability to create, edit and delete custom commands from chat:
//
//	!cmd add <name> <response>
//	!cmd edit <name> <response>
//	!cmd remove <name>
//
// Mutations are forwarded to the commands service's dashboard RPC (via
// engine.CommandManager) so sesame stays read-only on the projection layer.
func Cmd(d engine.Deps) module.Module {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	m := module.NewModule("", module.KindCore)

	m.Command("cmd").Mod().Run(func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		sub, rest := splitFirst(args)

		switch strings.ToLower(sub) {
		case "add":
			cmdAdd(ctx, c, d, rest, emit, log)
		case "edit":
			cmdEdit(ctx, c, d, rest, emit, log)
		case "remove", "delete":
			cmdRemove(ctx, c, d, rest, emit, log)
		default:
			reply(c, emit, i18n.T(c.Locale, "cmd.err.usage"), "", "")
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
		reply(c, emit, i18n.T(c.Locale, "cmd.err.missing_resp"), c.Env.ChatterUserLogin, "")
		return
	}

	// Guard: reject if the command already exists.
	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if _, found, _ := d.Proj.Command(ctx, c.BroadcasterID, name); found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.exists"), c.Env.ChatterUserLogin, name)
		return
	}

	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, name, response); err != nil {
		log.Warn("cmd: add failed", zap.String("name", name), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.added"), c.Env.ChatterUserLogin, name)
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
		reply(c, emit, i18n.T(c.Locale, "cmd.err.missing_resp"), c.Env.ChatterUserLogin, "")
		return
	}

	// Guard: reject if the command does not exist.
	name = strings.TrimPrefix(strings.ToLower(name), "!")
	if _, found, _ := d.Proj.Command(ctx, c.BroadcasterID, name); !found {
		reply(c, emit, i18n.T(c.Locale, "cmd.err.not_found"), c.Env.ChatterUserLogin, name)
		return
	}

	if err := d.Commands.Upsert(ctx, c.Env.BroadcasterUserID, name, response); err != nil {
		log.Warn("cmd: edit failed", zap.String("name", name), zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return
	}
	reply(c, emit, i18n.T(c.Locale, "cmd.modified"), c.Env.ChatterUserLogin, name)
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
	reply(c, emit, i18n.T(c.Locale, "cmd.removed"), c.Env.ChatterUserLogin, name)
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
