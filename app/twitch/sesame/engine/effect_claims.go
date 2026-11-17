// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/pkg/idempotency"
)

type completedEffect struct {
	ref       EffectRef
	handoff   *idempotency.Handoff
	err       error
	attempts  uint
	owner     string
	delivery  *effectDelivery
	ttl       time.Duration
	committed bool
}

func (d *EventDedup) queueCompleted(effect completedEffect) {
	d.mu.Lock()
	d.confirming = append(d.confirming, effect)
	start := !d.confirmRunning
	d.confirmRunning = true
	d.mu.Unlock()
	if start {
		go d.confirmEffects()
	}
}

func (d *EventDedup) confirmEffects() {
	for {
		time.Sleep(time.Millisecond)
		effects := d.drainCompleted()
		if len(effects) == 0 {
			return
		}
		d.confirmBatch(effects)
	}
}

func (d *EventDedup) drainCompleted() []completedEffect {
	d.mu.Lock()
	defer d.mu.Unlock()
	effects := d.confirming
	d.confirming = nil
	if len(effects) == 0 {
		d.confirmRunning = false
	}
	return effects
}

func (d *EventDedup) confirmBatch(effects []completedEffect) {
	eachEffectChunk(effects, d.settleEffects)
}

func eachEffectChunk(effects []completedEffect, apply func([]completedEffect)) {
	var workers sync.WaitGroup
	for start := 0; start < len(effects); {
		end := completedBatchEnd(effects, start)
		chunk := effects[start:end]
		workers.Go(func() { apply(chunk) })
		start = end
	}
	workers.Wait()
}

func (d *EventDedup) settleEffects(effects []completedEffect) {
	var committed, failed []completedEffect
	for _, effect := range effects {
		if effect.err != nil && !effect.committed {
			failed = append(failed, effect)
			continue
		}
		committed = append(committed, effect)
	}
	d.writeEffects(committed, "complete")
	d.writeEffects(failed, "release")
}

func (d *EventDedup) writeEffects(effects []completedEffect, mode string) {
	if len(effects) == 0 {
		return
	}
	states, err := d.updateEffects(effects, mode)
	if err != nil {
		d.retryEffects(effects)
		return
	}
	for i, effect := range effects {
		if mode == "complete" && states[i] != 1 {
			d.loseLease(effect)
		}
		d.removeEffect(effect)
		effect.handoff.Resolve(effect.err)
	}
}

func (d *EventDedup) retryEffects(effects []completedEffect) {
	for _, effect := range effects {
		d.retryEffect(effect)
	}
}

func (d *EventDedup) retryEffect(effect completedEffect) {
	if effect.delivery.sourceExpired() || !effect.delivery.alive() {
		d.removeEffect(effect)
		effect.handoff.Resolve(effect.err)
		return
	}
	delay := 100 * time.Millisecond * time.Duration(1<<min(effect.attempts, 4))
	effect.attempts = min(effect.attempts+1, 4)
	time.AfterFunc(delay, func() { d.queueCompleted(effect) })
}

func (d *EventDedup) updateEffects(effects []completedEffect, mode string) ([]int64, error) {
	return d.updateEffectsContext(context.Background(), effects, mode)
}

func (d *EventDedup) updateEffectsContext(ctx context.Context, effects []completedEffect, mode string) ([]int64, error) {
	keys := make([]idempotency.EffectKey, len(effects))
	owners := make([]string, len(effects))
	for i, effect := range effects {
		keys[i] = idempotency.EffectKey(dedupKey(effect.ref))
		owners[i] = effect.owner
	}
	ctx, cancel := context.WithTimeout(ctx, effectStoreTimeout)
	defer cancel()
	ttl := effectLeaseTTL
	if mode == "complete" {
		ttl = effects[0].ttl
	}
	states, err := idempotency.UpdateLeases(ctx, d.store, idempotency.LeaseBatch{Keys: keys, Owner: d.owner, Owners: owners, TTL: ttl, Mode: mode})
	if err != nil {
		d.unavailableUntil.Store(time.Now().Add(100 * time.Millisecond).UnixNano())
	}
	return states, err
}

func completedBatchEnd(effects []completedEffect, start int) int {
	end, bytes := start, 0
	for end < len(effects) && end-start < 256 {
		if effects[end].ttl != effects[start].ttl {
			break
		}
		size := len(effects[end].ref.Identity) + len(effects[end].ref.Effect) + 128
		if end > start && bytes+size > 256*1024 {
			break
		}
		bytes += size
		end++
	}
	return end
}
