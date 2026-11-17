// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"time"
)

type deliveryLivenessKey struct{}
type deliveryHoldKey struct{}
type deliveryRetentionKey struct{}
type deliveryExecutedKey struct{}
type deliverySourceExpiredKey struct{}
type deliveryCompletionKey struct{}
type deliveryFinalKey struct{}
type deliveryAsyncKey struct{}

func BeginDeliveryAsync(ctx context.Context) func() {
	change, ok := ctx.Value(deliveryAsyncKey{}).(func(int32))
	if !ok {
		return func() {}
	}
	change(1)
	return func() { change(-1) }
}

func DeliveryTerminal(ctx context.Context) bool {
	final, _ := ctx.Value(deliveryFinalKey{}).(bool)
	return final || DeliverySourceExpired(ctx)
}

func DeliverySourceExpired(ctx context.Context) bool {
	expired, ok := ctx.Value(deliverySourceExpiredKey{}).(func() bool)
	return ok && expired()
}

func DeliveryOnlyContext(ctx context.Context) context.Context {
	if ctx.Value(deliverySourceExpiredKey{}) == nil {
		return ctx
	}
	minimal := context.Background()
	for _, key := range []any{deliveryLivenessKey{}, deliverySourceExpiredKey{}, deliveryHoldKey{}, deliveryRetentionKey{}} {
		minimal = context.WithValue(minimal, key, ctx.Value(key))
	}
	return minimal
}

func EndDeliveryExecution(ctx context.Context) {
	if ended, ok := ctx.Value(deliveryExecutedKey{}).(func()); ok {
		ended()
	}
}

func HoldDelivery(ctx context.Context) {
	if hold, ok := ctx.Value(deliveryHoldKey{}).(func()); ok {
		hold()
	}
}

func DeliveryEffectRetention(ctx context.Context) time.Duration {
	if ttl, ok := ctx.Value(deliveryRetentionKey{}).(time.Duration); ok {
		return ttl
	}
	return 0
}

func DeliveryAlive(ctx context.Context) bool {
	alive, ok := ctx.Value(deliveryLivenessKey{}).(func() bool)
	if ok {
		return alive()
	}
	return ctx.Err() == nil
}

func (m *Message) WithDeliveryContext(ctx context.Context) context.Context {
	delivery := m.Context()
	ctx = context.WithValue(ctx, deliveryFinalKey{}, m.FinalDelivery())
	ctx = context.WithValue(ctx, deliveryHoldKey{}, func() { HoldDelivery(delivery) })
	ctx = context.WithValue(ctx, deliveryExecutedKey{}, func() { EndDeliveryExecution(delivery) })
	if async := delivery.Value(deliveryAsyncKey{}); async != nil {
		ctx = context.WithValue(ctx, deliveryAsyncKey{}, async)
	}
	ctx = context.WithValue(ctx, deliveryRetentionKey{}, DeliveryEffectRetention(delivery))
	completion := m.completionState()
	if attach, ok := delivery.Value(deliveryCompletionKey{}).(func(*Completion)); ok {
		attach(completion)
	}
	if expired := delivery.Value(deliverySourceExpiredKey{}); expired != nil {
		ctx = context.WithValue(ctx, deliverySourceExpiredKey{}, expired)
	}
	return context.WithValue(ctx, deliveryLivenessKey{}, func() bool {
		completion.mu.Lock()
		pending := completion.state == messagePending
		completion.mu.Unlock()
		return pending && DeliveryAlive(delivery)
	})
}

func (s *pullSubscriber) deliveryContext(ticket *DeliveryTicket) context.Context {
	reply, stored := ticket.reply, ticket.stored
	shard := s.pendingShard(reply)
	ctx := context.WithValue(s.laneContext(), deliveryHoldKey{}, func() { s.holdDelivery(reply, false) })
	ctx = context.WithValue(ctx, deliveryExecutedKey{}, func() { s.holdDelivery(reply, true) })
	ctx = context.WithValue(ctx, deliveryAsyncKey{}, func(delta int32) { s.changeDeliveryAsync(reply, delta) })
	ctx = context.WithValue(ctx, deliveryRetentionKey{}, max(TwitchEffectRetention(), time.Duration(s.horizon.Load())+s.desired.AckWait))
	ctx = context.WithValue(ctx, deliveryCompletionKey{}, func(c *Completion) {
		shard.Lock()
		delivery, ok := shard.deliveries[reply]
		if ok {
			delivery.completion = c
		}
		shard.Unlock()
	})
	ctx = context.WithValue(ctx, deliverySourceExpiredKey{}, func() bool {
		return s.sourceExpired(&DeliveryTicket{stored: stored}, time.Now())
	})
	return context.WithValue(ctx, deliveryLivenessKey{}, func() bool {
		shard.Lock()
		defer shard.Unlock()
		delivery, ok := shard.deliveries[reply]
		return s.pendingAlive(delivery, ok)
	})
}

func (s *pullSubscriber) changeDeliveryAsync(reply string, delta int32) {
	shard := s.pendingShard(reply)
	shard.Lock()
	defer shard.Unlock()
	if delivery := shard.deliveries[reply]; delivery != nil {
		delivery.async += delta
	}
}

func (s *pullSubscriber) pendingAlive(delivery *DeliveryTicket, exists bool) bool {
	if !exists || !s.deliveryHeartbeatAlive() {
		return false
	}
	return !s.progressExpired(delivery, time.Now())
}

func (s *pullSubscriber) holdDelivery(reply string, executed bool) {
	shard := s.pendingShard(reply)
	shard.Lock()
	defer shard.Unlock()
	delivery, ok := shard.deliveries[reply]
	if !ok || s.progressExpired(delivery, time.Now()) {
		return
	}
	if executed {
		delivery.executed = true
	} else {
		delivery.held = true
	}
}

func (s *pullSubscriber) deliveryHeartbeatAlive() bool {
	if !s.nc.IsConnected() {
		return false
	}
	select {
	case <-s.heartbeatDone:
		return false
	default:
		return true
	}
}

func TwitchEffectRetention() time.Duration {
	horizon := max(TwitchIngressStream.MaxAge, TwitchIngressPremiumStream.MaxAge, TwitchIngressStandardStream.MaxAge)
	return horizon + TwitchIngressRetryStream.MaxAge + pullAckWait()
}
