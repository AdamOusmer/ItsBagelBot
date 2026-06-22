// Package builtin holds the worker's shipped modules: the custom-command
// processor, the live tracker, and the bagel greeter. Each implements
// module.Module and is registered by the worker at startup.
package builtin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// CommandModule resolves a "!command" against the broadcaster's custom commands
// and, failing that, the shipped default-command table, then enforces the
// command's permission, live-only and cooldown gates before emitting the reply.
// It is a core module (always on); individual commands, not the processor, are
// what a broadcaster toggles.
type CommandModule struct {
	proj     projection.Reader
	live     module.LiveStore
	cooldown module.CooldownStore
	log      *zap.Logger
}

func NewCommandModule(proj projection.Reader, live module.LiveStore, cooldown module.CooldownStore, log *zap.Logger) *CommandModule {
	return &CommandModule{proj: proj, live: live, cooldown: cooldown, log: log}
}

func (m *CommandModule) Name() string     { return "" } // core: always on
func (m *CommandModule) Events() []string { return []string{"channel.chat.message"} }

func (m *CommandModule) Handle(ctx context.Context, c *module.Context) ([]*outgress.Message, error) {
	name, _, isCommand := parseCommand(c.Env.Text)
	if !isCommand {
		return nil, nil
	}

	cmd, found, err := m.resolve(ctx, c.BroadcasterID, name)
	if err != nil {
		return nil, err
	}
	if !found || cmd.Response == "" {
		return nil, nil
	}

	// Permission: an explicit allowed user overrides the role tier entirely.
	if cmd.AllowedUserID != 0 {
		if c.Env.ChatterUserID != strconv.FormatUint(cmd.AllowedUserID, 10) {
			return nil, nil
		}
	} else if !c.Chatter().Allows(module.ParsePerm(cmd.Perm)) {
		return nil, nil
	}

	if cmd.StreamOnlineOnly {
		live, err := m.live.IsLive(ctx, c.BroadcasterID)
		if err != nil {
			return nil, err
		}
		if !live {
			return nil, nil
		}
	}

	if cmd.Cooldown > 0 {
		key := "cooldown:cmd:" + strconv.FormatUint(c.BroadcasterID, 10) + ":" + name
		ok, err := m.cooldown.Allow(ctx, key, time.Duration(cmd.Cooldown)*time.Second)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}

	m.log.Debug("command matched",
		zap.String("command", name),
		zap.String("regress", c.Regress.String()),
		zap.Uint64("broadcaster_id", c.BroadcasterID),
	)
	return []*outgress.Message{chatReply(c.Env.BroadcasterUserID, cmd.Response)}, nil
}

// resolve finds the effective command: a user's custom command wins; otherwise a
// shipped default applies, with the broadcaster's reserved override (disable or
// reword) layered on top.
func (m *CommandModule) resolve(ctx context.Context, broadcasterID uint64, name string) (projection.Command, bool, error) {
	// Reserved system commands are owned by SystemModule and can never be
	// shadowed by a custom or default command.
	if isSystemCommand(name) {
		return projection.Command{}, false, nil
	}

	if cmd, found, err := m.proj.Command(ctx, broadcasterID, name); err != nil {
		return projection.Command{}, false, err
	} else if found {
		if !cmd.IsActive {
			return projection.Command{}, false, nil
		}
		return cmd, true, nil
	}

	def, ok := lookupDefault(name)
	if !ok {
		return projection.Command{}, false, nil
	}

	cmd := projection.Command{
		Name:             def.Name,
		Response:         def.Response,
		IsActive:         true,
		StreamOnlineOnly: def.StreamOnlineOnly,
		Perm:             def.Perm,
		Cooldown:         def.Cooldown,
	}

	enabled, response, err := m.override(ctx, broadcasterID, name)
	if err != nil {
		return projection.Command{}, false, err
	}
	if !enabled {
		return projection.Command{}, false, nil
	}
	if response != "" {
		cmd.Response = response
	}
	return cmd, true, nil
}

// override reads the reserved command.<name> ModuleView. A missing row means the
// shipped default applies, enabled; a present row's IsEnabled toggles the command
// and its Configs.response rewords it.
func (m *CommandModule) override(ctx context.Context, broadcasterID uint64, name string) (enabled bool, response string, err error) {
	mods, err := m.proj.Modules(ctx, broadcasterID)
	if err != nil {
		return false, "", err
	}
	want := defaultCommandPrefix + name
	for _, mv := range mods {
		if mv.Name != want {
			continue
		}
		if !mv.IsEnabled {
			return false, "", nil
		}
		var cfg struct {
			Response string `json:"response"`
		}
		if len(mv.Configs) > 0 {
			_ = json.Unmarshal(mv.Configs, &cfg)
		}
		return true, cfg.Response, nil
	}
	return true, "", nil // no row: default, enabled
}

// parseCommand extracts the command name and argument string from chat text.
// "!so @bob hi" -> ("so", "@bob hi", true). Non-commands return false.
func parseCommand(text string) (name, args string, ok bool) {
	trimmed := strings.TrimLeft(text, " ")
	if !strings.HasPrefix(trimmed, "!") {
		return "", "", false
	}
	body := strings.TrimPrefix(trimmed, "!")
	name, args, _ = strings.Cut(body, " ")
	if name == "" {
		return "", "", false
	}
	return strings.ToLower(name), strings.TrimSpace(args), true
}

// chatReply builds the outgress message that sends one chat line. sender_id is
// left for outgress to fill from the bot account it authenticates as.
func chatReply(broadcasterID, message string) *outgress.Message {
	body, _ := json.Marshal(map[string]string{
		"broadcaster_id": broadcasterID,
		"message":        message,
	})
	return &outgress.Message{
		Type:          outgress.TypeChat,
		BroadcasterID: broadcasterID,
		Endpoint:      "/helix/chat/messages",
		Method:        "POST",
		Payload:       body,
	}
}
