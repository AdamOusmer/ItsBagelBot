// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/idempotency"

	"go.uber.org/zap"
)

const (
	effectUse          = "use"
	EffectEarn         = "earn"
	EffectPointsAdjust = "padjust"
	EffectQuoteAdd     = "quoteadd"
	counterEffectPre   = "cbump:"
)

func CounterEffect(name string) string {
	return counterEffectPre + NormalizeCounterName(name)
}

type EventDedup struct {
	store  idempotency.Store
	prefix string
	ttl    time.Duration
	log    *zap.Logger
	dupes  atomic.Int64
}

func NewEventDedup(store idempotency.Store, prefix string, ttl time.Duration, log *zap.Logger) *EventDedup {
	if log == nil {
		log = zap.NewNop()
	}
	return &EventDedup{store: store, prefix: prefix, ttl: ttl, log: log}
}

func EventIdentity(env *lane.Envelope) string {
	if env.MsgID != "" {
		return env.MsgID
	}
	return env.EventID
}

type EffectRef struct {
	Identity string
	Effect   string
}

func (d *EventDedup) Duplicate(ctx context.Context, ref EffectRef) bool {
	if !d.active(ref.Identity) {
		return false
	}
	seen, _ := d.store.Seen(ctx, dedupKey(ref), d.ttl)
	if seen {
		d.dupes.Add(1)
	}
	return seen
}

func (d *EventDedup) Claim(ctx context.Context, ref EffectRef) (dup bool, release func()) {
	noop := func() {}
	if !d.active(ref.Identity) {
		return false, noop
	}
	key := dedupKey(ref)
	seen, err := d.store.Seen(ctx, key, d.ttl)
	if seen {
		d.dupes.Add(1)
		return true, noop
	}
	if err != nil {
		return false, noop
	}
	return false, func() { _ = d.store.Release(ctx, key) }
}

func (d *EventDedup) CounterClaim(env *lane.Envelope, name string) (string, time.Duration) {
	identity := EventIdentity(env)
	if !d.active(identity) {
		return "", 0
	}
	return idempotency.Key(d.prefix, dedupKey(EffectRef{Identity: identity, Effect: CounterEffect(name)})), d.ttl
}

func (d *EventDedup) Duplicates() int64 {
	if d == nil {
		return 0
	}
	return d.dupes.Load()
}

func (d *EventDedup) active(identity string) bool {
	return d != nil && d.store != nil && identity != ""
}

func dedupKey(ref EffectRef) string {
	return ref.Identity + ":" + ref.Effect
}

type CounterTarget struct {
	BroadcasterID uint64
	Name          string
	ViewerID      uint64
	Command       string
}

func CounterPeekValue(ctx context.Context, s LoyaltyStore, target CounterTarget) string {
	c, found, err := s.CounterPeek(ctx, target)
	if err != nil || !found {
		return ""
	}
	return strconv.FormatInt(c.Value, 10)
}
