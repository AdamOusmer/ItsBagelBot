// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
)

// fakeInviteResolver is an inviteResolver that returns a canned reply (or a
// canned error) per invite code, and counts how many times each code was
// resolved -- exactly what proves ownInvite's cache stops a repeat lookup
// for the same code.
type fakeInviteResolver struct {
	replies map[string]discordoutgress.InviteResolveReply
	err     error
	calls   map[string]int
}

func newFakeInviteResolver() *fakeInviteResolver {
	return &fakeInviteResolver{replies: map[string]discordoutgress.InviteResolveReply{}, calls: map[string]int{}}
}

func (f *fakeInviteResolver) ResolveInvite(_ context.Context, req discordoutgress.InviteResolveRequest) (discordoutgress.InviteResolveReply, error) {
	f.calls[req.Code]++
	if f.err != nil {
		return discordoutgress.InviteResolveReply{}, f.err
	}
	return f.replies[req.Code], nil
}

// memInviteCache is an in-memory inviteCache -- no Valkey, matching this
// package's other fakes (fakeGuard, fakeOwnInvite).
type memInviteCache struct {
	vals map[string]string
	ttls map[string]time.Duration
}

func newMemInviteCache() *memInviteCache {
	return &memInviteCache{vals: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (c *memInviteCache) Get(_ context.Context, code string) (string, bool) {
	v, ok := c.vals[code]
	return v, ok
}

func (c *memInviteCache) Put(_ context.Context, code, guildID string, ttl time.Duration) error {
	c.vals[code] = guildID
	c.ttls[code] = ttl
	return nil
}

func TestOwnInviteResolvesAndCachesPositive(t *testing.T) {
	resolver := newFakeInviteResolver()
	resolver.replies["abc"] = discordoutgress.InviteResolveReply{GuildID: "g1"}
	cache := newMemInviteCache()
	checker := NewOwnInviteChecker(resolver, cache)

	own, err := checker.IsOwnGuildInvite(context.Background(), "g1", "discord.gg/abc")
	if err != nil || !own {
		t.Fatalf("IsOwnGuildInvite = (%v, %v), want (true, nil)", own, err)
	}
	if resolver.calls["abc"] != 1 {
		t.Fatalf("resolver called %d times, want 1", resolver.calls["abc"])
	}
	if got, ttl := cache.vals["abc"], cache.ttls["abc"]; got != "g1" || ttl != invitePositiveTTL {
		t.Fatalf("cached (%q, %v), want (g1, %v)", got, ttl, invitePositiveTTL)
	}
}

// TestOwnInviteSecondLookupHitsCacheNotResolver is the "resolution is not
// attempted twice for a cached code" requirement: a second guild asking
// about the SAME code must be served from the cache, never a second RPC.
func TestOwnInviteSecondLookupHitsCacheNotResolver(t *testing.T) {
	resolver := newFakeInviteResolver()
	resolver.replies["abc"] = discordoutgress.InviteResolveReply{GuildID: "g1"}
	cache := newMemInviteCache()
	checker := NewOwnInviteChecker(resolver, cache)
	ctx := context.Background()

	if _, err := checker.IsOwnGuildInvite(ctx, "g1", "discord.gg/abc"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	own, err := checker.IsOwnGuildInvite(ctx, "g2", "discord.gg/abc")
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if own {
		t.Fatal("g2 asking about g1's invite got own=true, want false")
	}
	if resolver.calls["abc"] != 1 {
		t.Fatalf("resolver called %d times across two lookups of the same code, want 1 (the cache should have served the second)", resolver.calls["abc"])
	}
}

// TestOwnInviteNotFoundCachesNegative proves a dead/expired/group-DM code
// (outgress's NotFound) is remembered too, at inviteNegativeTTL -- a spam
// wave reposting the same dead code must not re-hit Discord on every
// repost.
func TestOwnInviteNotFoundCachesNegative(t *testing.T) {
	resolver := newFakeInviteResolver()
	resolver.replies["dead"] = discordoutgress.InviteResolveReply{NotFound: true}
	cache := newMemInviteCache()
	checker := NewOwnInviteChecker(resolver, cache)
	ctx := context.Background()

	own, err := checker.IsOwnGuildInvite(ctx, "g1", "discord.gg/dead")
	if err != nil || own {
		t.Fatalf("IsOwnGuildInvite = (%v, %v), want (false, nil)", own, err)
	}
	if got, ttl := cache.vals["dead"], cache.ttls["dead"]; got != "" || ttl != inviteNegativeTTL {
		t.Fatalf("cached (%q, %v), want (\"\", %v)", got, ttl, inviteNegativeTTL)
	}
	if _, err := checker.IsOwnGuildInvite(ctx, "g1", "discord.gg/dead"); err != nil {
		t.Fatalf("second lookup of a cached negative: %v", err)
	}
	if resolver.calls["dead"] != 1 {
		t.Fatalf("resolver called %d times for a repeatedly-posted dead code, want 1", resolver.calls["dead"])
	}
}

// TestOwnInviteRPCErrorNotCached proves an unresolvable answer (the RPC
// itself failed) is never cached, positive or negative -- caching it would
// let one transient outage's "unknown" wrongly stand in for a real answer
// for a full TTL. See resolveAndCache's doc.
func TestOwnInviteRPCErrorNotCached(t *testing.T) {
	resolver := newFakeInviteResolver()
	resolver.err = errors.New("nats timeout")
	cache := newMemInviteCache()
	checker := NewOwnInviteChecker(resolver, cache)
	ctx := context.Background()

	if _, err := checker.IsOwnGuildInvite(ctx, "g1", "discord.gg/abc"); err == nil {
		t.Fatal("want an error surfaced from a failed RPC")
	}
	if _, hit := cache.Get(ctx, "abc"); hit {
		t.Fatal("an unresolvable RPC error must not be cached")
	}
	if _, err := checker.IsOwnGuildInvite(ctx, "g1", "discord.gg/abc"); err == nil {
		t.Fatal("second lookup should still hit the resolver (nothing was cached), and still fail")
	}
	if resolver.calls["abc"] != 2 {
		t.Fatalf("resolver called %d times, want 2 (an error is never cached, so both lookups reach it)", resolver.calls["abc"])
	}
}

// TestOwnInviteReplyErrorNotCached is TestOwnInviteRPCErrorNotCached's
// twin for the other unresolvable shape: the RPC itself succeeded, but
// outgress could not classify the Discord call as found or not-found
// (InviteResolveReply.Error, e.g. a 5xx or the shared rate-limit bucket).
func TestOwnInviteReplyErrorNotCached(t *testing.T) {
	resolver := newFakeInviteResolver()
	resolver.replies["abc"] = discordoutgress.InviteResolveReply{Error: "discord: rate limited"}
	cache := newMemInviteCache()
	checker := NewOwnInviteChecker(resolver, cache)

	if _, err := checker.IsOwnGuildInvite(context.Background(), "g1", "discord.gg/abc"); err == nil {
		t.Fatal("want an error surfaced from Reply.Error")
	}
	if len(cache.vals) != 0 {
		t.Fatalf("cache = %v, want empty (a classified-error reply must not be cached)", cache.vals)
	}
}

// TestOwnInviteNonInviteLinkNeverResolves proves the guard in
// IsOwnGuildInvite itself (not just tripIsOwnInvite's caller-side check):
// a link linkguard.InviteCode does not recognize as an invite can never be
// "this guild's own invite" and must never reach the resolver or cache.
func TestOwnInviteNonInviteLinkNeverResolves(t *testing.T) {
	resolver := newFakeInviteResolver()
	cache := newMemInviteCache()
	checker := NewOwnInviteChecker(resolver, cache)

	own, err := checker.IsOwnGuildInvite(context.Background(), "g1", "https://example.com/not-an-invite")
	if err != nil || own {
		t.Fatalf("IsOwnGuildInvite = (%v, %v), want (false, nil)", own, err)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("resolver called for a non-invite link, want 0 calls")
	}
}
