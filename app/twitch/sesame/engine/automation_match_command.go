// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
)

const subMessageKind = "channel.subscription.message"

func (p *Pipeline) runAutomationCommand(ctx context.Context, c *module.Context, views map[string]projection.ModuleView, name, args string, emit module.Emit) {
	if p.automationsLocked(c) {
		return
	}
	for _, t := range p.channelIndex(ctx, c, views).Command(name) {
		if t.Command.Match != "exact" || args == "" {
			p.runChatTrigger(ctx, c, triggerMatch{plan: t.Plan, args: args, left: t.Left}, emit)
		}
	}
}

func (p *Pipeline) runChatTrigger(ctx context.Context, c *module.Context, m triggerMatch, emit module.Emit) {
	if m.left != nil {
		p.answerTimeLeft(ctx, c, m, emit)
		return
	}
	p.startAutomation(ctx, c, m, emit)
}
