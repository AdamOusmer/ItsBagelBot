// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func (l *loyaltyRPC) handleCooldownClaim(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	claim := repository.CooldownClaim{UserID: userID, Windows: make([]repository.CooldownWindow, len(req.Cooldowns))}
	for i, window := range req.Cooldowns {
		claim.Windows[i] = repository.CooldownWindow{Key: window.Key, TTL: time.Duration(window.TTLSeconds) * time.Second}
	}
	holds, won, err := l.repo.CooldownClaimAll(ctx, claim)
	if err != nil {
		return l.fail("loyalty cooldown.claim", err)
	}
	views := make([]loyaltyrpc.CooldownWindow, len(holds))
	for i, hold := range holds {
		views[i] = loyaltyrpc.CooldownWindow{Key: hold.Key, ExpiresAtMS: hold.ExpiresAtMS}
	}
	return loyaltyrpc.Reply{Claimed: won, Cooldowns: views}
}

func (l *loyaltyRPC) handleCooldownRelease(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	holds := make([]repository.CooldownHold, len(req.Cooldowns))
	for i, window := range req.Cooldowns {
		holds[i] = repository.CooldownHold{Key: window.Key, ExpiresAtMS: window.ExpiresAtMS}
	}
	if err := l.repo.CooldownRelease(ctx, userID, holds); err != nil {
		return l.fail("loyalty cooldown.release", err)
	}
	return loyaltyrpc.Reply{Found: true}
}
