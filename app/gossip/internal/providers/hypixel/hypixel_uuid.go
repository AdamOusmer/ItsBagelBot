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

type mojangProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

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

func (p *api) debitMojangUnlessUUID(ctx context.Context, account string, isPremium bool) error {
	if looksLikeUUID(strings.TrimSpace(account)) {
		return nil
	}
	return p.mojangBuckets.Enforce(ctx, p.limiter, isPremium)
}

func (p *api) lookupUUID(ctx context.Context, account string, isPremium bool) (string, error) {
	if err := p.debitMojangUnlessUUID(ctx, account, isPremium); err != nil {
		return "", err
	}
	return p.resolveUUID(ctx, account)
}
