// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
)

type effectPlanKey struct{}

type preparedEffect struct {
	effect completedEffect
	fresh  bool
	result chan leaseResult
}

func (p *Pipeline) prepareHandlerEffects(ctx context.Context, c *module.Context, views map[string]projection.ModuleView) context.Context {
	if !p.dedup.active(EventIdentity(&c.Env)) {
		return ctx
	}
	plan := map[EffectRef]preparedEffect{}
	for ordinal, m := range p.registry.For(c.Env.Type) {
		if !m.ProtectReplay || m.Events[c.Env.Type] == nil {
			continue
		}
		if !p.enabled(m, views, c) {
			continue
		}
		h := boundHandler{m: m, ordinal: ordinal}
		ref := EffectRef{Identity: EventIdentity(&c.Env), Effect: h.effectScope()}
		effect, fresh := p.dedup.reserveEffect(ctx, ref)
		prepared := preparedEffect{effect: effect, fresh: fresh}
		if fresh {
			prepared.result = p.dedup.enqueueAcquisition(ctx, effect)
		}
		plan[ref] = prepared
	}
	return context.WithValue(ctx, effectPlanKey{}, plan)
}

func (d *EventDedup) reserveForContext(ctx context.Context, ref EffectRef) (completedEffect, bool) {
	plan, _ := ctx.Value(effectPlanKey{}).(map[EffectRef]preparedEffect)
	if prepared, ok := plan[ref]; ok {
		return prepared.effect, prepared.fresh
	}
	return d.reserveEffect(ctx, ref)
}

func (d *EventDedup) firstAcquisition(ctx context.Context, effect completedEffect) (int64, error) {
	plan, _ := ctx.Value(effectPlanKey{}).(map[EffectRef]preparedEffect)
	prepared, ok := plan[effect.ref]
	if !ok {
		return d.acquireEffect(ctx, effect)
	}
	return awaitAcquisition(ctx, prepared.result)
}

func awaitAcquisition(ctx context.Context, result <-chan leaseResult) (int64, error) {
	select {
	case r := <-result:
		return r.state, r.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
