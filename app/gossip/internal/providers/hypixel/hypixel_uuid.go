// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hypixel

import (
	"context"
	"strings"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

// --- uuid resolution (Mojang) --------------------------------------------------

// mojangProfile is the api.mojang.com profile lookup body.
type mojangProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// looksLikeUUID reports whether account is already a uuid (32 hex chars,
// dashes optional), in which case Mojang is skipped.
func looksLikeUUID(account string) bool {
	n := 0
	for _, r := range account {
		switch {
		case r == '-':
			continue
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			n++
		default:
			return false
		}
	}
	return n == 32
}

// resolveUUID turns a username into the canonical uuid via Mojang, cached for
// a day. An unknown name is a 404 there already (204 on the legacy path is
// also treated as missing by the empty-id check), so it negative-caches.
// Callers (statsBudget, the uuid Handle) spend the Mojang budget before
// invoking this, so a uuid-shaped account never pays and a cached name
// still costs at the Handle — dashboard saves are rare enough that over-
// counting a hit is the safe direction.
func (p *api) resolveUUID(ctx context.Context, account string) (string, error) {
	if looksLikeUUID(account) {
		return strings.ReplaceAll(account, "-", ""), nil
	}
	key := core.Key(providerName, "uuid", accountKey(account))
	return core.Cached(ctx, p.cache, key, uuidTTL, negativeTTL, nil, func(ctx context.Context) (string, error) {
		var profile mojangProfile
		if err := p.mojang.GetJSON(ctx, "/users/profiles/minecraft/"+account, nil, &profile); err != nil {
			return "", err
		}
		if strings.TrimSpace(profile.ID) == "" {
			return "", &core.UpstreamError{Status: 404, Message: "player not found"}
		}
		return profile.ID, nil
	})
}

// uuid answers hypixel.uuid: the Mojang name→uuid binding, cached for a day.
// A raw uuid is returned canonical (undashed) without spending Mojang's
// budget. Friendly upstream failures (unknown name, throttle) ride the error
// envelope so the dashboard can keep the typed username when Mojang will not
// disclose the uuid.
func (p *api) uuid(ctx context.Context, req gossiprpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.HypixelUUIDReply{Error: "missing account"}
	}
	id, err := p.lookupUUID(ctx, account, req.IsPremium)
	if err != nil {
		return uuidErrReply(monitor.TxnLogger(ctx, p.log), account, err)
	}
	return gossiprpc.HypixelUUIDReply{Player: account, UUID: id}
}

func uuidErrReply(log *zap.Logger, account string, err error) gossiprpc.HypixelUUIDReply {
	msg, _ := core.FriendlyUpstream(err)
	if msg == "" {
		log.Warn("hypixel uuid fetch failed", zap.String("account", account), zap.Error(err))
		msg = "uuid lookup failed"
	}
	return gossiprpc.HypixelUUIDReply{Player: account, Error: msg}
}

// debitMojangUnlessUUID spends the Mojang profile budget unless account is
// already a uuid (no Mojang call). Shared by statsBudget and the uuid Handle
// so a stored linked-account uuid never taxes Mojang's per-IP allowance.
func (p *api) debitMojangUnlessUUID(ctx context.Context, account string, isPremium bool) error {
	if looksLikeUUID(account) {
		return nil
	}
	return p.mojangBuckets.Enforce(ctx, p.limiter, isPremium)
}

// lookupUUID spends the Mojang budget (when needed) then resolves. The Handle
// stays a thin envelope around this so missing-account and error shaping stay
// out of the debit/resolve path.
func (p *api) lookupUUID(ctx context.Context, account string, isPremium bool) (string, error) {
	if err := p.debitMojangUnlessUUID(ctx, account, isPremium); err != nil {
		return "", err
	}
	return p.resolveUUID(ctx, account)
}
