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

// inviteResolver is the one RPC call this file needs: resolve an invite
// code to the guild it targets (see internal/domain/rpc/discordoutgress's
// InviteResolveRequest doc for why that needs a real Discord round trip and
// cannot be expressed as a fire-and-forget Command).
type inviteResolver interface {
	ResolveInvite(ctx context.Context, req discordoutgress.InviteResolveRequest) (discordoutgress.InviteResolveReply, error)
}

// inviteCache is the subset of app/discord/engine/internal/invitecache.Cache
// this file calls through. An interface (rather than the concrete type)
// purely so linkguard_invite_test.go can substitute an in-memory fake with
// no Valkey round trip -- the same reason Guarder and inviteResolver are
// interfaces here too.
type inviteCache interface {
	Get(ctx context.Context, code string) (guildID string, hit bool)
	Put(ctx context.Context, code, guildID string, ttl time.Duration) error
}

// invitePositiveTTL/inviteNegativeTTL bound how long a resolved invite ->
// guild answer is trusted before this module asks outgress again.
//
// invitePositiveTTL is long (a day) because an invite code's TARGET guild
// is immutable for the life of the code -- Discord never retargets one
// invite to a different guild, it can only expire or get revoked, and
// either of those still resolves the same guild id right up until the
// eventual 404. A guild that pins its own invite keeps posting the SAME
// code indefinitely, so a day-long cache turns "resolve on every trip" into
// "resolve once a day at most" for that guild, with no correctness cost.
//
// inviteNegativeTTL reuses linkguard.Window (10 min) rather than inventing
// a second tuned number: a 404 (dead/expired/revoked code) is exactly the
// amplification case point 1 of linkguard.go's module doc exists to close
// -- a spam wave of junk invite codes plays out on Window's timescale (see
// Window's own doc), so caching a dead code for that long absorbs the
// entire wave behind one REST call per code. Re-checking after Window has
// passed costs nothing extra: a code that is still dead just 404s again
// and gets re-cached.
const (
	invitePositiveTTL = 24 * time.Hour
	inviteNegativeTTL = linkguard.Window
)

// OwnInviteChecker tells whether rawLink -- already known to be a Discord
// invite (linkguard.NormalizeLink's isInvite) -- targets guildID itself.
//
// It is consulted from exactly one place, tripIsOwnInvite in linkguard.go,
// and ONLY for a Verdict that already tripped a threshold: resolving on
// every invite link posted would turn a spam wave of junk codes into an
// equal-sized wave of outgress REST calls against the shared ~50 req/s
// bot-token budget (see linkguard.go's module doc, point 1). A trip is
// rare; a message is not.
type OwnInviteChecker interface {
	IsOwnGuildInvite(ctx context.Context, guildID, rawLink string) (bool, error)
}

// ownInvite is the production OwnInviteChecker: outgress's invite-resolve
// RPC, fronted by a Valkey cache so the SAME code is never resolved twice
// (see invitePositiveTTL/inviteNegativeTTL above).
type ownInvite struct {
	resolver inviteResolver
	cache    inviteCache
}

// NewOwnInviteChecker builds the production checker. resolver and cache are
// both expected live (main.go wires them right next to rpcclient.New and
// invitecache.New, the same pattern linkguard.New itself uses -- see that
// constructor's own doc for why this package does not defensively nil-guard
// its collaborators).
func NewOwnInviteChecker(resolver inviteResolver, cache inviteCache) OwnInviteChecker {
	return ownInvite{resolver: resolver, cache: cache}
}

// IsOwnGuildInvite resolves rawLink's invite code to its target guild and
// compares it to guildID. A link NormalizeLink would not tag as an invite
// (ok false from linkguard.InviteCode) can never be "this guild's own
// invite" by definition, so that case returns false with no RPC or cache
// lookup at all -- callers already only reach this method for an
// invite-shaped link (see OwnInviteChecker's doc), but this guard keeps the
// method correct even if that changes.
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

// resolveAndCache is the cache-miss path: call outgress, cache the answer
// (positive or a confirmed negative), and report the comparison. An error
// from the RPC itself, or one outgress reports classified (Reply.Error,
// meaning it could not tell found from not-found -- see
// InviteResolveReply's doc), is returned uncached: caching an unresolvable
// answer would let one transient outage's worth of "unknown" wrongly stand
// in for "not the guild's own" (or vice versa) for a full TTL. The caller
// (tripIsOwnInvite in linkguard.go) decides what an error means for the
// pending delete; this method only ever reports a definite yes/no once it
// actually has one.
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
