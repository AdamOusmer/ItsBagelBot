// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/automation"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
)

const longestValkeyCooldown = automation.MaxValkeyCooldownSeconds * time.Second

type LongCooldownClaim struct {
	BroadcasterID uint64
	Windows       []CooldownWindow
}

// LongCooldownHold lists the won windows after a win and the refusing window after a refusal.
type LongCooldownHold struct {
	BroadcasterID uint64
	Windows       []modulesrpc.CooldownWindow
}

type LongCooldownStore interface {
	ClaimLongCooldowns(ctx context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error)
	ReleaseLongCooldowns(ctx context.Context, hold LongCooldownHold) error
	PeekLongCooldowns(ctx context.Context, claim LongCooldownClaim) ([]modulesrpc.CooldownWindow, error)
	// ExtendLongCooldown adds addSeconds to a named window's expiry. found is false when the window is not running; capped marks a result the system ceiling shortened.
	ExtendLongCooldown(ctx context.Context, broadcasterID uint64, key string, addSeconds int64) (expiresAtMS int64, found bool, capped bool, err error)
}

func (c *RecipeStateRPC) cooldownCall(ctx context.Context, verb string, req modulesrpc.CooldownRequest) (modulesrpc.CooldownReply, error) {
	return recipeStateCall[modulesrpc.CooldownReply](ctx, c, "cooldown."+verb, req)
}

func (c *RecipeStateRPC) ClaimLongCooldowns(ctx context.Context, claim LongCooldownClaim) (LongCooldownHold, bool, error) {
	windows := make([]modulesrpc.CooldownWindow, len(claim.Windows))
	for i, window := range claim.Windows {
		windows[i] = modulesrpc.CooldownWindow{Key: window.Key, TTLSeconds: int64(window.TTL / time.Second)}
	}
	reply, err := c.cooldownCall(ctx, "claim", modulesrpc.CooldownRequest{UserID: fmtID(claim.BroadcasterID), Windows: windows})
	if err != nil {
		return LongCooldownHold{}, false, err
	}
	return LongCooldownHold{BroadcasterID: claim.BroadcasterID, Windows: reply.Windows}, reply.Claimed, nil
}

func (c *RecipeStateRPC) ReleaseLongCooldowns(ctx context.Context, hold LongCooldownHold) error {
	_, err := c.cooldownCall(ctx, "release", modulesrpc.CooldownRequest{UserID: fmtID(hold.BroadcasterID), Windows: hold.Windows})
	return err
}

func (c *RecipeStateRPC) PeekLongCooldowns(ctx context.Context, claim LongCooldownClaim) ([]modulesrpc.CooldownWindow, error) {
	windows := make([]modulesrpc.CooldownWindow, len(claim.Windows))
	for i, window := range claim.Windows {
		windows[i] = modulesrpc.CooldownWindow{Key: window.Key}
	}
	reply, err := c.cooldownCall(ctx, "peek", modulesrpc.CooldownRequest{UserID: fmtID(claim.BroadcasterID), Windows: windows})
	if err != nil {
		return nil, err
	}
	return reply.Windows, nil
}

// ExtendLongCooldown reuses TTLSeconds on the window to carry the seconds to add, not a duration.
func (c *RecipeStateRPC) ExtendLongCooldown(ctx context.Context, broadcasterID uint64, key string, addSeconds int64) (int64, bool, bool, error) {
	req := modulesrpc.CooldownRequest{UserID: fmtID(broadcasterID), Windows: []modulesrpc.CooldownWindow{{Key: key, TTLSeconds: addSeconds}}}
	reply, err := c.cooldownCall(ctx, "extend", req)
	if err != nil {
		return 0, false, false, err
	}
	if !reply.Found || len(reply.Windows) == 0 {
		return 0, false, false, nil
	}
	return reply.Windows[0].ExpiresAtMS, true, reply.Capped, nil
}

// The viewer window goes first in long: a viewer it refuses then never touches the shared global row.
// A named window stays long at any length: its row is the one cooldown_add extends, for as long as it runs.
func splitCooldownWindows(windows []CooldownWindow) (short, long []CooldownWindow) {
	for _, window := range windows {
		if window.TTL > longestValkeyCooldown || automation.IsNamedCooldownKey(window.Key) {
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
