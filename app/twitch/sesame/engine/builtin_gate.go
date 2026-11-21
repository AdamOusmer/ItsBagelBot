// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/codec"
)

// The built-ins on the commands page store their access override alongside
// their toggle and reply in the module row. Resolve it before the common gate,
// including for aliases, so a restricted command never claims a cooldown or
// reaches its handler for an unauthorized chatter.
func (p *Pipeline) builtinGate(ctx context.Context, c *module.Context, cmd module.Command) (gateRule, error) {
	rule := gateRule{
		name: cmd.Name, allowedUserID: cmd.AllowedUserID, perm: cmd.Perm,
		liveOnly: cmd.LiveOnly, cooldown: cmd.Cooldown, userCooldown: cmd.UserCooldown,
	}
	if !configurableBuiltin(cmd.Name) || p.proj == nil {
		return rule, nil
	}
	view, found, err := p.proj.Module(ctx, c.BroadcasterID, cmd.Name)
	if err != nil || !found {
		return rule, err
	}
	if cmd.Name == voiceplan.ChatCommand {
		c.Config = view.Configs
	}
	return rule, parseBuiltinGate(&rule, view.Configs)
}

type builtinOverrides struct {
	Permission   string           `json:"permission"`
	Cooldown     codec.RawMessage `json:"cooldown"`
	UserCooldown codec.RawMessage `json:"userCooldown"`
}

func parseBuiltinGate(rule *gateRule, raw codec.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var config builtinOverrides
	if err := codec.Unmarshal(raw, &config); err != nil {
		return err
	}
	if err := rule.overridePermission(config.Permission); err != nil {
		return err
	}
	if rule.name != voiceplan.ChatCommand {
		return nil
	}
	if err := overrideCooldown(config.Cooldown, &rule.cooldown); err != nil {
		return err
	}
	return overrideCooldown(config.UserCooldown, &rule.userCooldown)
}

func (r *gateRule) overridePermission(permission string) error {
	switch permission {
	case "":
		return nil
	case "everyone", "sub", "vip", "mod", "lead_mod", "broadcaster":
		r.perm = module.ParsePerm(permission)
		return nil
	default:
		// A corrupt override must not quietly become the Everyone tier.
		return fmt.Errorf("invalid built-in permission for %s", r.name)
	}
}

func overrideCooldown(raw codec.RawMessage, window *time.Duration) error {
	if len(raw) == 0 {
		return nil
	}
	var seconds string
	if err := codec.Unmarshal(raw, &seconds); err != nil {
		return err
	}
	value, err := strconv.ParseUint(seconds, 10, 32)
	if err != nil || validate.Cooldown(uint(value)) != nil {
		return validate.ErrCooldownInvalid
	}
	*window = time.Duration(value) * time.Second
	return nil
}

func configurableBuiltin(name string) bool {
	switch name {
	case "accountage", "followage", "uptime", "clip", voiceplan.ChatCommand, "title", "game", "tags", "commercial", "marker":
		return true
	default:
		return false
	}
}
