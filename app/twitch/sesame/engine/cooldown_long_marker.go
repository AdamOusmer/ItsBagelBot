// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/automation"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// MarkedLongCooldowns answers refusals from Valkey so a live window costs about one database read per day. A marker hit refuses whichever store wrote it, because the hourly move leaves a window only in Valkey.
type MarkedLongCooldowns struct {
	client valkey.Client
	store  LongCooldownStore
	log    *zap.Logger
}

func NewMarkedLongCooldowns(client valkey.Client, store LongCooldownStore, log *zap.Logger) *MarkedLongCooldowns {
	return &MarkedLongCooldowns{client: client, store: store, log: log.Named("long-cooldowns")}
}

func (m *MarkedLongCooldowns) ClaimLongCooldowns(ctx context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error) {
	if m.marked(ctx, claim) {
		return LongCooldownHold{}, false, nil
	}
	hold, won, err := m.store.ClaimLongCooldowns(ctx, claim)
	if err != nil {
		return LongCooldownHold{}, false, err
	}
	if !won {
		m.remark(ctx, hold)
		return LongCooldownHold{}, false, nil
	}
	if !m.markWon(ctx, hold) {
		return hold, true, nil
	}
	return LongCooldownHold{}, false, m.ReleaseLongCooldowns(context.WithoutCancel(ctx), hold)
}

func (m *MarkedLongCooldowns) ReleaseLongCooldowns(ctx context.Context, hold LongCooldownHold) error {
	err := m.store.ReleaseLongCooldowns(ctx, hold)
	for _, window := range hold.Windows {
		key := automation.LongCooldownMarkerKey(hold.BroadcasterID, window.Key)
		owner := automation.LongCooldownMarkerValue(window.ExpiresAtMS)
		if clearErr := pkg_valkey.NewOwnerLock(m.client, key, owner).Release(ctx); clearErr != nil {
			m.log.Warn("long cooldown marker not cleared", zap.String("key", key), zap.Error(clearErr))
		}
	}
	return err
}

// A Valkey failure falls through to the database, which still decides every window it holds.
func (m *MarkedLongCooldowns) marked(ctx context.Context, claim LongCooldownClaim) bool {
	keys := make([]string, len(claim.Windows))
	for i, window := range claim.Windows {
		keys[i] = automation.LongCooldownMarkerKey(claim.BroadcasterID, window.Key)
	}
	count, err := m.client.Do(ctx, m.client.B().Exists().Key(keys...).Build()).AsInt64()
	if err != nil {
		m.log.Warn("long cooldown marker read failed, asking the database", zap.Error(err))
		return false
	}
	return count > 0
}

func (m *MarkedLongCooldowns) remark(ctx context.Context, hold LongCooldownHold) {
	for _, window := range hold.Windows {
		ttl := automation.LongCooldownMarkerTTL(window.ExpiresAtMS, time.Now())
		if ttl <= 0 {
			continue
		}
		cmd := m.client.B().Set().Key(automation.LongCooldownMarkerKey(hold.BroadcasterID, window.Key)).
			Value(automation.LongCooldownMarkerValue(window.ExpiresAtMS)).Nx().Px(ttl).Build()
		if err := m.client.Do(ctx, cmd).Error(); err != nil && !valkey.IsValkeyNil(err) {
			m.log.Warn("long cooldown marker write failed", zap.Error(err))
		}
	}
}

// markWon reports a lost race: the hourly job moved a live window out from under this claim between the marker read and the database win, so the row this claim took proves nothing. A concurrent refused claimer may have already written this very expiry, which is not a loss.
func (m *MarkedLongCooldowns) markWon(ctx context.Context, hold LongCooldownHold) bool {
	lost := false
	for _, window := range hold.Windows {
		lost = m.markWindow(ctx, hold.BroadcasterID, window) || lost
	}
	return lost
}

func (m *MarkedLongCooldowns) markWindow(ctx context.Context, channel uint64, window modulesrpc.CooldownWindow) bool {
	value := automation.LongCooldownMarkerValue(window.ExpiresAtMS)
	ttl := automation.LongCooldownMarkerTTL(window.ExpiresAtMS, time.Now())
	if ttl <= 0 {
		return false
	}
	cmd := m.client.B().Set().Key(automation.LongCooldownMarkerKey(channel, window.Key)).Value(value).Nx().Get().Px(ttl).Build()
	held, err := m.client.Do(ctx, cmd).ToString()
	if valkey.IsValkeyNil(err) {
		return false
	}
	if err != nil {
		m.log.Warn("long cooldown marker write failed", zap.Error(err))
		return false
	}
	return held != value
}
