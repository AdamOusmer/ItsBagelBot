// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/lane"
)

func (p *Pipeline) recordDecoded(env *lane.Envelope) {
	if userChat(env) {
		p.analytics.Add(catalog.ChatMessages, int64(env.MessageCount()))
	}
}

func userChat(env *lane.Envelope) bool {
	return env.Type == chatType && env.Origin != "trial"
}

// recordProcessed runs after a live envelope: answered counts commands, active counts any output.
func (p *Pipeline) recordProcessed(broadcasterID uint64, accountCreatedAtMicros int64, answered bool, outputs int) {
	if answered {
		p.analytics.Count(catalog.ChatAnswered)
	}
	if !answered && outputs == 0 {
		return
	}
	p.analytics.Distinct(catalog.ChannelsActive, broadcasterID)
	if accountCreatedAtMicros > 0 {
		p.analytics.Cohort(broadcasterID, time.UnixMicro(accountCreatedAtMicros))
	}
}

func newlyEngaged(err error, before, after bool) bool {
	return err == nil && after && !before
}

func (p *Pipeline) recordBaked(c *module.Context, bc BoundCommand, engaged bool) {
	if !engaged {
		return
	}
	p.analytics.Count(catalog.CommandUses, bc.Cmd.Name)
	p.analytics.Distinct(catalog.ModuleActive, c.BroadcasterID, moduleLabel(bc.Owner))
	p.analytics.Distinct(catalog.CommandChannels, c.BroadcasterID, bc.Cmd.Name)
}

func (p *Pipeline) recordHandled(mctx *module.Context, m module.Module, emittedBefore int) {
	if mctx.Emitted > emittedBefore {
		label := moduleLabel(m)
		p.analytics.Count(catalog.ModuleEvents, label)
		p.analytics.Distinct(catalog.ModuleActive, mctx.BroadcasterID, label)
	}
}
