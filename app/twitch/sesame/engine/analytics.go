// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/lane"
)

func (p *Pipeline) recordAcknowledged(ctx context.Context, env *lane.Envelope, err error) {
	if err == nil && userChat(env) {
		p.analytics.AddContext(ctx, catalog.ChatMessages, int64(env.MessageCount()))
	}
}

func userChat(env *lane.Envelope) bool {
	return env.Type == chatType && env.Origin != "trial"
}

type processedMetrics struct {
	channel  uint64
	answered bool
	created  int64
	outputs  int
}

func (p *Pipeline) recordProcessed(ctx context.Context, m processedMetrics) {
	if m.answered {
		p.analytics.AddContext(ctx, catalog.ChatAnswered, 1)
	}
	if !m.answered && m.outputs == 0 {
		return
	}
	p.analytics.DistinctContext(ctx, catalog.ChannelsActive, m.channel)
	if m.created > 0 {
		created := time.UnixMicro(m.created)
		p.analytics.CohortContext(ctx, m.channel, created)
		p.analytics.DistinctContext(ctx, catalog.ActivationAge, m.channel, activationAge(created, time.Now()))
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

func (p *Pipeline) recordBaked(ctx context.Context, c *module.Context, bc BoundCommand, emittedBefore int) {
	if !countsOutputs(c, emittedBefore) {
		return
	}
	id, name := c.BroadcasterID, bc.Cmd.Name
	p.recordCommandAnalytics(ctx, c, func(ctx context.Context) {
		p.analytics.AddContext(ctx, catalog.CommandUses, 1, name)
		p.analytics.DistinctContext(ctx, catalog.CommandChannels, id, name)
	})
	p.recordModuleOutputs(ctx, c, bc.analyticsModule(), emittedBefore)
}

func (p *Pipeline) recordCommandAnalytics(ctx context.Context, c *module.Context, record func(context.Context)) {
	if p.analytics == nil {
		return
	}
	record(ctx)
}

func (p *Pipeline) recordProcessedEffect(ctx context.Context, c *module.Context, created int64, outputs int) {
	snapshot := processedMetrics{channel: c.BroadcasterID, answered: c.CommandAnswered, created: created, outputs: outputs}
	p.recordProcessedMetrics(ctx, snapshot)
}

func (p *Pipeline) recordProcessedMetrics(ctx context.Context, m processedMetrics) {
	if m.answered && p.stats != nil {
		p.stats.record(ctx, func() { p.stats.countAnswered(m.channel) })
	}
	p.recordProcessed(ctx, m)
}

func (p *Pipeline) recordHandled(ctx context.Context, mctx *module.Context, m module.Module, emittedBefore int) {
	p.recordModuleOutputs(ctx, mctx, moduleLabel(m), emittedBefore)
}

func countsOutputs(c *module.Context, emittedBefore int) bool {
	return c.Env.Origin != "trial" && c.Emitted > emittedBefore
}

func (p *Pipeline) recordModuleOutputs(ctx context.Context, c *module.Context, label string, emittedBefore int) {
	if p.analytics == nil || !countsOutputs(c, emittedBefore) {
		return
	}
	p.analytics.AddContext(ctx, catalog.ModuleEvents, int64(c.Emitted-emittedBefore), label)
	p.analytics.DistinctContext(ctx, catalog.ModuleActive, c.BroadcasterID, label)
}

func (p *Pipeline) recordBlocked(ctx context.Context, c *module.Context, reason string, err error) {
	if c.Env.Origin != "trial" && err == nil {
		p.analytics.AddContext(ctx, catalog.CommandBlocked, 1, reason)
	}
}
