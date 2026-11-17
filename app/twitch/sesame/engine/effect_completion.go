// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	"github.com/google/uuid"
)

type effectShard struct {
	sync.Mutex
	effects map[EffectRef]*pendingEffect
	local   map[EffectRef]*idempotency.Handoff
}

type pendingEffect struct {
	handoff  *idempotency.Handoff
	delivery *effectDelivery
	lost     bool
	owner    string
	ttl      time.Duration
}

type effectBusy struct{}

func (effectBusy) Error() string             { return "event effect owned by another worker" }
func (effectBusy) RetryAfter() time.Duration { return 10 * time.Second }

func (d *EventDedup) shard(ref EffectRef) *effectShard {
	var hash uint32
	for i := range ref.Identity {
		hash = hash*31 + uint32(ref.Identity[i])
	}
	return &d.shards[hash%uint32(len(d.shards))]
}

func (d *EventDedup) Effect(ctx context.Context, ref EffectRef, run func(context.Context) error) (err error) {
	if !d.active(ref.Identity) {
		return run(ctx)
	}
	ref.Identity, ref.Effect = strings.Clone(ref.Identity), strings.Clone(ref.Effect)
	effect, fresh := d.reserveForContext(ctx, ref)
	confirmed := effect.handoff
	idempotency.Join(ctx, confirmed)
	if !fresh {
		return nil
	}
	state, err := d.acquireForContext(ctx, effect)
	if err != nil || state != 0 {
		return d.resolveAcquisition(effect, leaseResult{state: state, err: err})
	}
	if !d.beginEffect(effect) {
		d.removeEffect(effect)
		confirmed.Resolve(effectBusy{})
		return effectBusy{}
	}
	child, w := idempotency.NewWork(ctx)
	w.Watch(func(r idempotency.Result) { d.finishEffect(effect, r) })
	defer func() {
		if !idempotency.Tracking(ctx) && !w.Deferred() {
			waitEffect(ctx, confirmed)
		}
	}()
	defer w.SealOnReturn(&err)
	err = run(child)
	effect.committed = err == nil
	return err
}

func (d *EventDedup) resolveAcquisition(effect completedEffect, result leaseResult) error {
	d.removeEffect(effect)
	err := result.err
	if result.state == 2 {
		err = effectBusy{}
	}
	effect.handoff.Resolve(err)
	return err
}

func (d *EventDedup) acquireForContext(ctx context.Context, effect completedEffect) (int64, error) {
	state, err := d.firstAcquisition(ctx, effect)
	for (leaseResult{state: state, err: err}).waitForPeer(ctx) {
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
		state, err = d.acquireEffect(ctx, effect)
	}
	return state, err
}

func (r leaseResult) waitForPeer(ctx context.Context) bool {
	return r.err == nil && r.state == 2 && !idempotency.Tracking(ctx)
}

func (d *EventDedup) reserveEffect(ctx context.Context, ref EffectRef) (completedEffect, bool) {
	ctx = bus.DeliveryOnlyContext(ctx)
	s := d.shard(ref)
	s.Lock()
	defer s.Unlock()
	if handoff := s.local[ref]; handoff != nil {
		return completedEffect{ref: ref, handoff: handoff}, false
	}
	if entry := s.effects[ref]; entry != nil && entry.renewable() {
		return completedEffect{ref: ref, handoff: entry.handoff, owner: entry.owner}, false
	}
	if s.effects == nil {
		s.effects = make(map[EffectRef]*pendingEffect)
	}
	h := &idempotency.Handoff{}
	owner := d.owner + ":" + uuid.NewString()
	ttl := bus.DeliveryEffectRetention(ctx)
	if ttl == 0 {
		ttl = d.ttl
	}
	delivery := &effectDelivery{ctx: ctx}
	s.effects[ref] = &pendingEffect{handoff: h, delivery: delivery, owner: owner, ttl: ttl}
	return completedEffect{ref: ref, handoff: h, owner: owner, delivery: delivery, ttl: ttl}, true
}

func (d *EventDedup) beginEffect(effect completedEffect) bool {
	s := d.shard(effect.ref)
	s.Lock()
	defer s.Unlock()
	entry := s.effects[effect.ref]
	if entry == nil {
		return false
	}
	if entry.handoff != effect.handoff {
		return false
	}
	if entry.lost || !entry.delivery.alive() {
		return false
	}
	return true
}

func waitEffect(ctx context.Context, h *idempotency.Handoff) {
	done := make(chan struct{})
	h.Watch(func(r idempotency.Result) {
		if r.Final {
			close(done)
		}
	})
	select {
	case <-ctx.Done():
	case <-done:
	}
}

func (d *EventDedup) removeEffect(effect completedEffect) {
	s := d.shard(effect.ref)
	s.Lock()
	entry := s.effects[effect.ref]
	if entry != nil && entry.handoff == effect.handoff {
		delete(s.effects, effect.ref)
	}
	if s.local[effect.ref] == effect.handoff {
		delete(s.local, effect.ref)
	}
	s.Unlock()
}

func (d *EventDedup) finishEffect(effect completedEffect, r idempotency.Result) {
	if !r.Final {
		return
	}
	if effect.committed {
		effect.delivery.hold()
	}
	effect.err = r.Err
	if effect.delivery.sourceExpired() {
		d.removeEffect(effect)
		effect.handoff.Resolve(r.Err)
		return
	}
	d.queueCompleted(effect)
}
