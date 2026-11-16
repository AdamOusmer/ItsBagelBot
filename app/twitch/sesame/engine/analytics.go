// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/lane"
)

func (p *Pipeline) recordAcknowledged(env *lane.Envelope, err error) {
	if err == nil && userChat(env) {
		p.analytics.Add(catalog.ChatMessages, int64(env.MessageCount()))
	}
}

func userChat(env *lane.Envelope) bool {
	return env.Type == chatType && env.Origin != "trial"
}

func (p *Pipeline) recordProcessed(broadcasterID uint64, accountCreatedAtMicros int64, answered bool, outputs int) {
	if answered {
		p.analytics.Count(catalog.ChatAnswered)
	}
	if !answered && outputs == 0 {
		return
	}
	p.analytics.Distinct(catalog.ChannelsActive, broadcasterID)
	if accountCreatedAtMicros > 0 {
		created := time.UnixMicro(accountCreatedAtMicros)
		p.analytics.Cohort(broadcasterID, created)
		p.analytics.Distinct(catalog.ActivationAge, broadcasterID, activationAge(created, time.Now()))
	}
}

func activationAge(created, now time.Time) string {
	days := int(now.Sub(created).Hours() / 24)
	switch {
	case days <= 0:
		return "d0"
	case days == 1:
		return "d1"
	case days < 7:
		return "d2_6"
	case days < 14:
		return "d7_13"
	case days < 30:
		return "d14_29"
	default:
		return "d30p"
	}
}

func (bc BoundCommand) analyticsModule() string {
	if bc.Owner.Name == "" && configurableBuiltin(bc.Cmd.Name) {
		return bc.Cmd.Name
	}
	return moduleLabel(bc.Owner)
}

func (p *Pipeline) recordBaked(c *module.Context, bc BoundCommand, emittedBefore int) {
	if c.Env.Origin == "trial" || c.Emitted <= emittedBefore {
		return
	}
	p.analytics.Count(catalog.CommandUses, bc.Cmd.Name)
	p.recordModuleOutputs(c, bc.analyticsModule(), emittedBefore)
	p.analytics.Distinct(catalog.CommandChannels, c.BroadcasterID, bc.Cmd.Name)
}

func (p *Pipeline) recordHandled(mctx *module.Context, m module.Module, emittedBefore int) {
	p.recordModuleOutputs(mctx, moduleLabel(m), emittedBefore)
}

func (p *Pipeline) recordModuleOutputs(c *module.Context, label string, emittedBefore int) {
	if c.Env.Origin == "trial" || c.Emitted <= emittedBefore {
		return
	}
	p.analytics.Add(catalog.ModuleEvents, int64(c.Emitted-emittedBefore), label)
	p.analytics.Distinct(catalog.ModuleActive, c.BroadcasterID, label)
}

func (p *Pipeline) recordBlocked(c *module.Context, reason string, err error) {
	if c.Env.Origin != "trial" && err == nil {
		p.analytics.Count(catalog.CommandBlocked, reason)
	}
}
