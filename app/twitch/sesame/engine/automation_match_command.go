// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/projection"
)

const subMessageKind = "channel.subscription.message"

type recipeCommand struct {
	input commandInput
	views map[string]projection.ModuleView
}

func (p *Pipeline) runAutomationCommand(ctx context.Context, c *module.Context, cmd recipeCommand, emit module.Emit) {
	if p.automationsLocked(c) {
		return
	}
	for _, t := range p.channelIndex(ctx, c, cmd.views).Command(c.Command) {
		if matchesDispatchedCommand(t.Command, cmd.input) {
			p.runChatTrigger(ctx, c, triggerMatch{plan: t.Plan, args: cmd.input.args, left: t.Left, countdownLeft: t.CountdownLeft}, emit)
		}
	}
}

func matchesDispatchedCommand(trigger automation.CommandTrigger, input commandInput) bool {
	if trigger.Match == "exact" && input.args != "" {
		return false
	}
	name := "!" + input.name
	return !trigger.CaseSensitive || trigger.Name == name || slices.Contains(trigger.Aliases, name)
}

func (p *Pipeline) runChatTrigger(ctx context.Context, c *module.Context, m triggerMatch, emit module.Emit) {
	if m.countdownLeft != nil {
		p.answerCountdownLeft(ctx, c, m, emit)
		return
	}
	if m.left != nil {
		p.answerTimeLeft(ctx, c, m, emit)
		return
	}
	p.startAutomation(ctx, c, m, emit)
}
