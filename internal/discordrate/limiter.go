// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discordrate pays Discord's global rate limit before any REST call
// any Discord service makes.
//
// internal/discordapi.Client parses a Discord 429 into RateLimitError but
// never waits or throttles on it -- that decision belongs to the caller, one
// layer up, because only the caller knows whether a wait is affordable
// (an interaction defer is perishable; a guild fill can afford to sleep).
// Before app/dingress split into ingress/engine/outgress, dingress's gateway
// role was the caller and paid nothing: it called the REST client directly
// with no bucket at all. Outgress's Twitch chat lanes did pay one, through
// pkg/ratelimit against a fleet-shared Valkey key (ratelimit:discord:global;
// see the deleted app/twitch/outgress/internal/worker/discord.go's
// takeDiscordGlobal for the original).
//
// Discord's global limit is per BOT TOKEN, not per process. After the
// three-way split, two processes hold this package's Gate: app/discord/
// ingress (only for the inline INTERACTION_CREATE defer) and
// app/discord/outgress (everything else -- welcomes, tickets, go-live
// embeds, guild fills). The bucket has to be shared infrastructure, not a
// per-process in-memory counter -- an in-memory limiter in each pod would
// let the fleet send Nx the real budget. Reusing pkg/ratelimit's
// Valkey-backed token bucket (rather than inventing a second limiter) is
// what makes that sharing free: same Spec, same key, same Lua script every
// other bucket in the repo already relies on.
package discordrate

import (
	"context"
	"errors"

	"ItsBagelBot/pkg/ratelimit"

	"github.com/valkey-io/valkey-go"
)

const (
	// globalCapacity/globalWindow reproduce outgress's discordGlobalSpec
	// exactly: 45 requests per second against the bot's real 50 req/s global
	// budget, leaving headroom so bursts never touch Discord's global 429.
	// The two roles paying the SAME numbers against the SAME key is the
	// whole point -- a mismatched Spec on one side would let that side
	// starve or overrun the other (see pkg/ratelimit's package doc: every
	// caller of the same key must use the same Spec).
	globalCapacity = 45.0
	globalWindow   = 1.0

	// globalKey is the fleet-wide Valkey bucket key. It must match the
	// value outgress used (ratelimit:discord:global) so a REST call
	// gateway, egress, or a not-yet-migrated outgress process makes all
	// draw from the same bucket during the transition.
	globalKey = "ratelimit:discord:global"
)

var globalSpec = ratelimit.NewSpec(globalCapacity, globalCapacity/globalWindow)

// ErrRateLimited is returned when the shared bucket is empty. Callers treat
// it like any other transient Discord failure: log and skip (the announcer
// paths) or surface it (the RPC handlers).
var ErrRateLimited = errors.New("dingress: discord global rate limit")

// Gate pays one token before a REST call. It is an interface (rather than
// *Limiter directly) so tests can substitute a fake that never touches
// Valkey -- this repo's tests never hit live network.
type Gate interface {
	Take(ctx context.Context) error
}

// Limiter is the production Gate: one shared Valkey token bucket.
type Limiter struct {
	client *ratelimit.Limiter
}

// New builds the shared-bucket gate. Both ROLE=gateway and ROLE=egress
// construct one of these off their own Valkey client and hand it to
// NewLimitedClient; there is exactly one bucket (by key), not one per role.
func New(client valkey.Client) *Limiter {
	return &Limiter{client: ratelimit.New(client)}
}

// Take consumes one token from the fleet-wide bucket, or reports that none
// were available.
func (l *Limiter) Take(ctx context.Context) error {
	allowed, err := l.client.Allow(ctx, globalSpec.ForKey(globalKey))
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}
