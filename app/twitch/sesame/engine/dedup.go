// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	"github.com/google/uuid"

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
	mu               sync.Mutex
	shards           [32]effectShard
	owner            string
	acquiring        []effectAcquisition
	acquireRunning   bool
	heartbeatRunning bool
	confirming       []completedEffect
	confirmRunning   bool
	store            idempotency.Store
	prefix           string
	ttl              time.Duration
	log              *zap.Logger
	dupes            atomic.Int64
	conflicts        atomic.Int64
	unavailableUntil atomic.Int64
	stopped          atomic.Bool
	activity         atomic.Uint64
}

func NewEventDedup(store idempotency.Store, prefix string, ttl time.Duration, log *zap.Logger) *EventDedup {
	if log == nil {
		log = zap.NewNop()
	}
	return &EventDedup{store: store, prefix: prefix, ttl: max(ttl, bus.TwitchEffectRetention()), log: log, owner: uuid.NewString()}
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
