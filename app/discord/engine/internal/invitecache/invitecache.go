// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package invitecache remembers one Discord invite code's target guild id
// in Valkey, so linkguard's own-invite resolution
// (app/discord/engine/modules/linkguard.go's ownInvite) never asks
// outgress's invite-resolve RPC twice for the same code. An invite that
// resolves once should stay resolved for as long as the answer stays true
// (see New's doc for the TTLs), and a code that does not resolve at all
// (revoked, expired, never valid) needs to be remembered too -- otherwise a
// spam wave of junk codes re-hits Discord's REST API, through outgress's
// shared ~50 req/s bot-token budget, once per repost.
//
// This is deliberately NOT folded into internal/discordstore even though it
// shares that package's key-shape convention ("discord:<thing>:<id>"):
// discordstore is cross-process state outgress and engine both read/write
// (see its own doc), while an invite's target guild is trivia only this one
// engine module ever needs. Adding it to discordstore's shared interface
// (and its in-memory test double) would widen a contract nothing outside
// linkguard touches.
package invitecache

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// noGuild marks a cached negative resolution -- the code does not resolve
// to any guild, whether because Discord returned 404 or because it
// resolved to a group-DM invite with no guild at all (see
// discordoutgress.InviteResolveReply's doc for why both collapse the same
// way). It is stored instead of an empty string because Valkey's GET
// returns "" both when a key is missing and when "" was itself the stored
// value -- an empty string would make a cached negative indistinguishable
// from a cache miss, and Get would then re-resolve every negative result
// forever, defeating the whole point of caching one.
const noGuild = "\x00none"

// Cache is the Valkey-backed invite code -> guild id cache.
type Cache struct {
	client valkey.Client
}

// New builds the cache. Like linkguard.New (internal/domain/discord/
// linkguard.New), this does NOT nil-guard client: engine's main.go already
// Fatals before valkeyClient can be nil, so a defensive nil check here
// would just hide that invariant breaking instead of surfacing it.
func New(client valkey.Client) *Cache {
	return &Cache{client: client}
}

func key(code string) string { return "discord:invite:" + code + ":guild" }

// Get reports the cached answer for code. hit is false for a genuine cache
// miss (nothing stored, or expired) -- callers must call outgress's
// invite-resolve RPC in that case. hit is true for BOTH a resolved guild id
// and a cached negative result, which is why guildID alone cannot signal a
// miss: guildID == "" with hit == true means "confirmed, no guild",
// distinct from hit == false, "unknown, go ask".
func (c *Cache) Get(ctx context.Context, code string) (guildID string, hit bool) {
	raw, err := c.client.Do(ctx, c.client.B().Get().Key(key(code)).Build()).ToString()
	if err != nil || raw == "" {
		return "", false
	}
	if raw == noGuild {
		return "", true
	}
	return raw, true
}

// Put remembers guildID (or "" for a confirmed negative) for ttl. The
// caller (ownInvite.IsOwnGuildInvite) picks ttl per outcome -- see
// linkguard.go's invitePositiveTTL/inviteNegativeTTL doc -- and never calls
// Put at all for an unresolvable RPC/Discord error, since that answer is
// not safe to remember (see the same doc for why).
func (c *Cache) Put(ctx context.Context, code, guildID string, ttl time.Duration) error {
	val := guildID
	if val == "" {
		val = noGuild
	}
	return c.client.Do(ctx, c.client.B().Set().Key(key(code)).Value(val).Ex(ttl).Build()).Error()
}
