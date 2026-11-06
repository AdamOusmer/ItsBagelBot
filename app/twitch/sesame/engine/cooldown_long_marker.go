// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"

	"ItsBagelBot/internal/domain/cooldownmarker"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// MarkedLongCooldowns answers refusals from Valkey so a live window costs about one database read per day. A marker hit refuses whichever store wrote it, because the hourly move leaves a window only in Valkey.
type MarkedLongCooldowns struct {
	markers *cooldownmarker.Store
	store   LongCooldownStore
	log     *zap.Logger
}

func NewMarkedLongCooldowns(client valkey.Client, store LongCooldownStore, log *zap.Logger) *MarkedLongCooldowns {
	return &MarkedLongCooldowns{markers: cooldownmarker.New(client), store: store, log: log.Named("long-cooldowns")}
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
		if clearErr := m.markers.Clear(ctx, markerOf(hold.BroadcasterID, window)); clearErr != nil {
			m.log.Warn("long cooldown marker not cleared", zap.String("window", window.Key), zap.Error(clearErr))
		}
	}
	return err
}

// PeekLongCooldowns reads each marker's stored expiry, which outlasts a marker capped at a day, and only asks the store for windows with no marker.
func (m *MarkedLongCooldowns) PeekLongCooldowns(ctx context.Context, claim LongCooldownClaim) ([]modulesrpc.CooldownWindow, error) {
	live := make([]modulesrpc.CooldownWindow, 0, len(claim.Windows))
	missing := LongCooldownClaim{BroadcasterID: claim.BroadcasterID}
	for _, window := range claim.Windows {
		expiry, marked := m.markerExpiry(ctx, claim.BroadcasterID, window.Key)
		if marked {
			live = append(live, modulesrpc.CooldownWindow{Key: window.Key, ExpiresAtMS: expiry})
			continue
		}
		missing.Windows = append(missing.Windows, window)
	}
	if len(missing.Windows) == 0 {
		return live, nil
	}
	stored, err := m.store.PeekLongCooldowns(ctx, missing)
	if err != nil {
		return nil, err
	}
	m.remark(ctx, LongCooldownHold{BroadcasterID: claim.BroadcasterID, Windows: stored})
	return append(live, stored...), nil
}

func markerOf(channel uint64, window modulesrpc.CooldownWindow) cooldownmarker.Marker {
	return cooldownmarker.Marker{Channel: channel, Window: window.Key, ExpiresAtMS: window.ExpiresAtMS}
}

func (m *MarkedLongCooldowns) markerExpiry(ctx context.Context, channel uint64, window string) (int64, bool) {
	value, found, err := m.markers.Expiry(ctx, channel, window)
	if err != nil {
		m.log.Warn("long cooldown marker read failed, asking the database", zap.Error(err))
	}
	return value, found
}

// A Valkey failure falls through to the database, which still decides every window it holds.
func (m *MarkedLongCooldowns) marked(ctx context.Context, claim LongCooldownClaim) bool {
	windows := make([]string, len(claim.Windows))
	for i, window := range claim.Windows {
		windows[i] = window.Key
	}
	live, err := m.markers.AnyLive(ctx, claim.BroadcasterID, windows)
	if err != nil {
		m.log.Warn("long cooldown marker read failed, asking the database", zap.Error(err))
		return false
	}
	return live
}

func (m *MarkedLongCooldowns) remark(ctx context.Context, hold LongCooldownHold) {
	for _, window := range hold.Windows {
		m.markWindow(ctx, hold.BroadcasterID, window)
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
	lost, err := m.markers.Mark(ctx, markerOf(channel, window))
	if err != nil {
		m.log.Warn("long cooldown marker write failed", zap.Error(err))
	}
	return lost
}
