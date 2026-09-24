// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/discord/linkguard"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
)

type inviteResolver interface {
	ResolveInvite(ctx context.Context, req discordoutgress.InviteResolveRequest) (discordoutgress.InviteResolveReply, error)
}

type inviteCache interface {
	Get(ctx context.Context, code string) (guildID string, hit bool)
	Put(ctx context.Context, code, guildID string, ttl time.Duration) error
}

const (
	invitePositiveTTL = 24 * time.Hour
	inviteNegativeTTL = linkguard.Window
)

type OwnInviteChecker interface {
	IsOwnGuildInvite(ctx context.Context, guildID, rawLink string) (bool, error)
}

type ownInvite struct {
	resolver inviteResolver
	cache    inviteCache
}

func NewOwnInviteChecker(resolver inviteResolver, cache inviteCache) OwnInviteChecker {
	return ownInvite{resolver: resolver, cache: cache}
}

func (o ownInvite) IsOwnGuildInvite(ctx context.Context, guildID, rawLink string) (bool, error) {
	code, ok := linkguard.InviteCode(rawLink)
	if !ok {
		return false, nil
	}
	if target, hit := o.cache.Get(ctx, code); hit {
		return target != "" && target == guildID, nil
	}
	return o.resolveAndCache(ctx, code, guildID)
}

// Never cache an error: a wrong cached answer deletes or spares invites for a whole TTL.
func (o ownInvite) resolveAndCache(ctx context.Context, code, guildID string) (bool, error) {
	reply, err := o.resolver.ResolveInvite(ctx, discordoutgress.InviteResolveRequest{Code: code})
	if err != nil {
		return false, err
	}
	if reply.Error != "" {
		return false, errors.New(reply.Error)
	}
	if reply.NotFound {
		_ = o.cache.Put(ctx, code, "", inviteNegativeTTL)
		return false, nil
	}
	_ = o.cache.Put(ctx, code, reply.GuildID, invitePositiveTTL)
	return reply.GuildID == guildID, nil
}
