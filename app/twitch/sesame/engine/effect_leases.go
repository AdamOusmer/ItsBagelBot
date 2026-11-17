// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"ItsBagelBot/pkg/idempotency"
	"go.uber.org/zap"
)

const effectLeaseTTL = 12 * time.Second
const effectStoreTimeout = 75 * time.Millisecond

type effectAcquisition struct {
	effect completedEffect
	result chan leaseResult
	ctx    context.Context
}

type leaseResult struct {
	state int64
	err   error
}

func (d *EventDedup) acquireEffect(ctx context.Context, effect completedEffect) (int64, error) {
	return awaitAcquisition(ctx, d.enqueueAcquisition(ctx, effect))
}

func (d *EventDedup) enqueueAcquisition(ctx context.Context, effect completedEffect) chan leaseResult {
	a := effectAcquisition{effect: effect, result: make(chan leaseResult, 1), ctx: ctx}
	if time.Now().UnixNano() < d.unavailableUntil.Load() {
		a.result <- leaseResult{err: errors.New("effect store temporarily unavailable")}
		return a.result
	}
	d.mu.Lock()
	d.acquiring = append(d.acquiring, a)
	start := !d.acquireRunning
	d.acquireRunning = true
	d.mu.Unlock()
	d.activity.Add(1)
	if start {
		go d.acquireEffects()
	}
	d.startHeartbeat()
	return a.result
}

func (d *EventDedup) startHeartbeat() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.heartbeatRunning {
		return
	}
	d.heartbeatRunning = true
	go d.heartbeat()
}

func (d *EventDedup) heartbeat() {
	ticker := time.NewTicker(effectLeaseTTL / 3)
	defer ticker.Stop()
	for range ticker.C {
		generation := d.activity.Load()
		effects := d.pendingEffects()
		d.renewEffects(effects)
		pending := len(effects) > 0
		d.mu.Lock()
		d.heartbeatRunning = pending || generation != d.activity.Load() && !d.stopped.Load()
		running := d.heartbeatRunning
		d.mu.Unlock()
		if !running {
			return
		}
	}
}

func (d *EventDedup) acquireEffects() {
	for {
		time.Sleep(500 * time.Microsecond)
		d.mu.Lock()
		batch := d.acquiring
		d.acquiring = nil
		d.acquireRunning = len(batch) != 0
		d.mu.Unlock()
		if len(batch) == 0 {
			return
		}
		d.acquireBatch(batch)
	}
}

func (d *EventDedup) acquireBatch(batch []effectAcquisition) {
	effects := make([]completedEffect, len(batch))
	for i, a := range batch {
		effects[i] = a.effect
	}
	var workers sync.WaitGroup
	for start := 0; start < len(effects); {
		end := completedBatchEnd(effects, start)
		chunk, pending := batch[start:end], effects[start:end]
		workers.Go(func() { d.acquireChunk(chunk, pending) })
		start = end
	}
	workers.Wait()
}

func (d *EventDedup) acquireChunk(batch []effectAcquisition, effects []completedEffect) {
	states, err := d.updateEffects(effects, "acquire")
	for i, a := range batch {
		r := leaseResult{err: err}
		if err == nil {
			r.state = states[i]
		}
		a.result <- r
	}
}

func (d *EventDedup) pendingEffects() []completedEffect {
	return d.effectSnapshot(true)
}

func (d *EventDedup) effectSnapshot(liveOnly bool) []completedEffect {
	var effects []completedEffect
	var expired []completedEffect
	for i := range d.shards {
		s := &d.shards[i]
		s.Lock()
		for ref, entry := range s.effects {
			if liveOnly && entry.delivery.sourceExpired() {
				expired = append(expired, completedEffect{ref: ref, owner: entry.owner, ttl: entry.ttl})
				s.retainLocal(ref, entry)
				continue
			}
			if liveOnly && !entry.renewable() {
				continue
			}
			effects = append(effects, completedEffect{ref: ref, handoff: entry.handoff, owner: entry.owner, delivery: entry.delivery, ttl: entry.ttl})
		}
		s.Unlock()
	}
	eachEffectChunk(expired, func(effects []completedEffect) { _, _ = d.updateEffects(effects, "release") })
	return effects
}

func (s *effectShard) retainLocal(ref EffectRef, entry *pendingEffect) {
	entry.delivery.compact()
	if s.local == nil {
		s.local = make(map[EffectRef]*idempotency.Handoff)
	}
	s.local[ref] = entry.handoff
	delete(s.effects, ref)
}

func (entry *pendingEffect) renewable() bool {
	return !entry.lost && entry.delivery.alive()
}

func (d *EventDedup) renewEffects(effects []completedEffect) {
	eachEffectChunk(effects, d.renewChunk)
}

func (d *EventDedup) renewChunk(effects []completedEffect) {
	states, err := d.updateEffects(effects, "renew")
	if err != nil {
		d.log.Warn("effect lease renewal failed", zap.Error(err))
		return
	}
	for i, state := range states {
		if state != 0 {
			d.loseLease(effects[i])
		}
	}
}

func (d *EventDedup) loseLease(effect completedEffect) {
	d.markLeaseLost(effect)
	d.conflicts.Add(1)
	d.log.Warn("effect lease lost", zap.String("identity", effect.ref.Identity), zap.String("effect", effect.ref.Effect))
}

func (d *EventDedup) markLeaseLost(effect completedEffect) {
	ref := effect.ref
	s := d.shard(ref)
	s.Lock()
	entry := s.effects[ref]
	if entry != nil && entry.handoff == effect.handoff {
		entry.lost = true
	}
	s.Unlock()
}

func (d *EventDedup) Finish(ctx context.Context) {
	if d == nil {
		return
	}
	d.stopped.Store(true)
}

func (d *EventDedup) LeaseConflicts() int64 { return d.conflicts.Load() }
