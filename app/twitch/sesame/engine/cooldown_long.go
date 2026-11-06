// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/automation"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

const longestValkeyCooldown = automation.MaxValkeyCooldownSeconds * time.Second

type LongCooldownClaim struct {
	BroadcasterID uint64
	Windows       []CooldownWindow
}

type LongCooldownHold struct {
	BroadcasterID uint64
	Windows       []loyaltyrpc.CooldownWindow
}

type LongCooldownStore interface {
	ClaimLongCooldowns(ctx context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error)
	ReleaseLongCooldowns(ctx context.Context, hold LongCooldownHold) error
}

func (l *LoyaltyRPC) ClaimLongCooldowns(ctx context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error) {
	windows := make([]loyaltyrpc.CooldownWindow, len(claim.Windows))
	for i, window := range claim.Windows {
		windows[i] = loyaltyrpc.CooldownWindow{Key: window.Key, TTLSeconds: int64(window.TTL / time.Second)}
	}
	reply, err := l.call(ctx, "cooldown.claim", loyaltyrpc.Request{UserID: fmtID(claim.BroadcasterID), Cooldowns: windows})
	if err != nil {
		return LongCooldownHold{}, false, err
	}
	return LongCooldownHold{BroadcasterID: claim.BroadcasterID, Windows: reply.Cooldowns}, reply.Claimed, nil
}

func (l *LoyaltyRPC) ReleaseLongCooldowns(ctx context.Context, hold LongCooldownHold) error {
	_, err := l.call(ctx, "cooldown.release", loyaltyrpc.Request{UserID: fmtID(hold.BroadcasterID), Cooldowns: hold.Windows})
	return err
}

// The viewer window goes first in long: a viewer it refuses then never touches the shared global row.
func splitCooldownWindows(windows []CooldownWindow) (short, long []CooldownWindow) {
	for _, window := range windows {
		if window.TTL > longestValkeyCooldown {
			long = append([]CooldownWindow{window}, long...)
		} else {
			short = append(short, window)
		}
	}
	return short, long
}

// Long rows go first because most attempts on a long window are refused there, before any Valkey key is spent.
func (p *Pipeline) claimCooldownWindows(ctx context.Context, broadcasterID uint64, windows []CooldownWindow) (bool, error) {
	short, long := splitCooldownWindows(windows)
	if len(long) == 0 {
		return p.claimValkeyWindows(ctx, short)
	}
	store := p.automations.LongCooldowns
	if store == nil {
		return false, nil
	}
	hold, won, err := store.ClaimLongCooldowns(ctx, LongCooldownClaim{BroadcasterID: broadcasterID, Windows: long})
	if err != nil || !won {
		return false, err
	}
	won, err = p.claimValkeyWindows(ctx, short)
	if err == nil && won {
		return true, nil
	}
	return false, errors.Join(err, store.ReleaseLongCooldowns(context.WithoutCancel(ctx), hold))
}

func (p *Pipeline) claimValkeyWindows(ctx context.Context, windows []CooldownWindow) (bool, error) {
	switch len(windows) {
	case 0:
		return true, nil
	case 1:
		return p.cooldown.Allow(ctx, windows[0].Key, windows[0].TTL)
	default:
		return p.cooldown.AllowAll(ctx, windows)
	}
}
