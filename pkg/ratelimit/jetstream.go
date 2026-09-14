// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"math"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/kvstate"
)

// JetStreamManager shares one authoritative bucket across all replicas. There
// is no backend failover: Valkey availability cannot create a second budget.
type JetStreamManager struct{ store kvstate.Store }

func NewJetStreamManager(store kvstate.Store) *JetStreamManager {
	return &JetStreamManager{store: store}
}

type durableTokens struct {
	Tokens float64
	At     time.Time
}
type durableBudget map[string]durableTokens

func (m *JetStreamManager) Allow(ctx context.Context, req Request) (bool, error) {
	denied, err := m.allow(ctx, req, []Request{req})
	return denied == 0 && err == nil, err
}

func (m *JetStreamManager) AllowOrdered(ctx context.Context, first, second Request) (uint8, error) {
	return m.allow(ctx, second, []Request{first, second})
}

func requestID(req Request) string { id := req.bucketID(); return id.Scope + "\x00" + id.Value }

func (m *JetStreamManager) allow(ctx context.Context, shared Request, requests []Request) (uint8, error) {
	var denied uint8
	_, err := kvstate.Change(ctx, m.store, kvstate.Key(requestID(shared)), func(old kvstate.Value) ([]byte, error) {
		state := durableBudget{}
		if len(old.Data) != 0 {
			if err := codec.Unmarshal(old.Data, &state); err != nil {
				return nil, err
			}
		}
		denied = evaluateBudget(state, requests, old.Created)
		return codec.Marshal(state)
	})
	return denied, err
}

// The previous revision's broker timestamp is the clock, never a pod's wall
// clock. Refill lags by one decision, deliberately conservative. Even denials
// advance the revision so paced redelivery can observe newly earned tokens.
func evaluateBudget(state durableBudget, requests []Request, now time.Time) uint8 {
	var denied uint8
	for i, req := range requests {
		key := requestID(req)
		bucket, exists := state[key]
		capacity := float64(req.Spec.capacity)
		if !exists {
			bucket = durableTokens{Tokens: capacity, At: now}
		}
		if bucket.At.IsZero() {
			bucket.At = now
		}
		elapsed := math.Max(0, now.Sub(bucket.At).Seconds())
		bucket.Tokens = math.Min(capacity, bucket.Tokens+elapsed*req.Spec.refillPerSec)
		if now.After(bucket.At) {
			bucket.At = now
		}
		state[key] = bucket
		if bucket.Tokens < 1 && denied == 0 {
			denied = uint8(i + 1)
		}
	}
	if denied == 0 {
		spendBudget(state, requests)
	}
	return denied
}

func spendBudget(state durableBudget, requests []Request) {
	for _, req := range requests {
		key := requestID(req)
		bucket := state[key]
		bucket.Tokens--
		state[key] = bucket
	}
}
