// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
)

type AutomationHook struct {
	pipeline *Pipeline
}

func (h *AutomationHook) Wire(p *Pipeline) {
	if h != nil {
		h.pipeline = p
	}
}

// Chat is registered so chat events load module views; chat triggers themselves run from the command and scan stages.
func (h *AutomationHook) Register(b *module.Builder) {
	b.On(chatType, h.Event(chatType))
	for _, kind := range automation.EventKinds() {
		b.On(kind.Kind, h.Event(kind.Kind))
	}
}

func (h *AutomationHook) Event(kind string) module.EventHandler {
	return func(ctx context.Context, mctx *module.Context, emit module.Emit) error {
		if h != nil && h.pipeline != nil {
			h.pipeline.runKindAutomations(ctx, mctx, kind, emit)
		}
		return nil
	}
}
