// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"

	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// mintLeaseKeyPrefix namespaces the distributed mint lock in Valkey,
// consistent with this service's other SET-NX locks (see
// internal/channels/registry.go's enrollLockPrefix / modCheckLockPrefix,
// which use the same "outgress:<thing>:" shape).
const mintLeaseKeyPrefix = "outgress:token:mint:"

// mintLeaseTTL bounds how long one replica may hold the per-account mint
// lock before Valkey expires it and another replica is allowed to try.
//
// REQUIRED INVARIANT: mintLeaseTTL must exceed the maximum time a mint can
// legally hold the lease, or the TTL can lapse while the winner is still
// inside its own mint -- at which point a second replica acquires the "free"
// lease and mints concurrently, which is exactly the multi-redemption of
// Twitch's rotating refresh token this lease exists to prevent. This used to
// be violated: the old 5s TTL sat under tokenClientTimeout (10s) alone, and
// the lease was held until every one of persistAttempts=3 retries at
// persistTimeout=5s plus persistRetryBackoff=200ms had run -- a real
// worst-case hold of roughly 10s + 3*5s + 2*0.2s ~= 25.4s, five times the
// TTL meant to bound it.
//
// The fix has two parts, both in app/outgress/internal/twitch/token.go:
//  1. tokenClientTimeout (the mint request's own bound) dropped from a blind
//     10s to 5s, ~11.6x the measured 2026-08-20 worst case (430ms for the
//     network path; see its doc for why real Twitch-side grant processing on
//     top of that was never measured and isn't assumed here).
//  2. mintLeased now releases the lease after the FIRST Persist attempt
//     resolves, not after all persistAttempts retries -- see mintLeased's
//     doc for the reasoning and the trade-off that accepts.
//
// Those two together give twitch.MaxMintLeaseHold() = tokenClientTimeout +
// persistTimeout = 5s + 5s = 10s, computed from the same constants at
// runtime (see TestMintLeaseTTLExceedsMaxHold). 15s gives that a 1.5x
// margin -- comfortably above the worst case with room to spare, while
// staying far shorter than backgroundRefreshInterval (1m) or refreshMargin
// (5m), so a replica that crashes mid-mint (holding the lease) only stalls
// that account's renewal for a short, bounded window before the next lazy
// Token() call or background tick retries.
//
// WHY A LONGER TTL IS SAFER HERE THAN IT LOOKS: raising this does not make a
// losing replica wait longer. A loser never blocks on mintLeaseTTL directly
// -- it polls for up to leaseWaitAttempts*leaseWaitInterval (~2s, see
// token.go), and on every poll it also tries to acquire the lease itself
// (see waitForLeaseOrAdoption), so a winner that crashes frees the lease and
// gets noticed well within that ~2s budget regardless of how large
// mintLeaseTTL is. mintLeaseTTL only matters for the one scenario where
// nobody is polling fast enough to notice liveness on their own -- Valkey's
// own expiry as the final backstop -- so a larger margin there costs nothing
// in the common path and only helps the crash-detection backstop be
// unambiguously safe.
const mintLeaseTTL = 15 * time.Second

// mintLeaseReleaseTimeout bounds the best-effort release call, which
// deliberately runs on a background context (see newMintLease): the request
// or refresh that triggered the mint may already be done and its ctx
// cancelled by the time Persist finishes. Matches the 2s budget used for the
// other lock releases in this service (worker/batch.go, worker/mod_status.go).
const mintLeaseReleaseTimeout = 2 * time.Second

// newMintLease builds the distributed lease twitch.NewStoredUserTokenSource
// uses to serialize real mints for one account across outgress's 3 replicas
// -- see twitch.MintLease's doc for why: Twitch rotates the refresh token on
// redemption, so two replicas minting the same one is destructive, not just
// wasteful. Keyed by account id (bot user id or broadcaster id), not one
// global lock, so unrelated accounts never queue behind each other.
//
// twitch stays free of a Valkey import: this lives in main, built over
// deps.valkey, and is injected the same way StoredTokenIO already is.
func (d *deps) newMintLease(accountID string) twitch.MintLease {
	key := mintLeaseKeyPrefix + accountID
	holder := d.host
	client := d.valkey
	log := d.log

	return twitch.MintLease{
		// The unavailable return distinguishes "Valkey itself is
		// unreachable" from "another replica holds the lock" -- see
		// twitch.MintLease.Acquire's doc for why callers must not wait out
		// the adoption poll budget in the former case: nobody can be
		// coordinating through a backend nobody can read, so that wait is
		// guaranteed wasted and mintOrAdopt skips straight to an
		// uncoordinated mint instead.
		Acquire: func(ctx context.Context) (func(), bool, bool) {
			ok, err := acquireValkeyLock(ctx, client, key, holder, mintLeaseTTL)
			if err != nil {
				log.Warn("mint lease backend unavailable; minting immediately, uncoordinated",
					zap.String("account_id", accountID), zap.Error(err))
				return nil, false, true
			}
			if !ok {
				return nil, false, false
			}
			return func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), mintLeaseReleaseTimeout)
				defer cancel()
				if err := releaseValkeyLock(releaseCtx, client, key, holder); err != nil {
					log.Warn("mint lease release failed; it will expire on its own",
						zap.String("account_id", accountID), zap.Error(err))
				}
			}, true, false
		},
	}
}

// acquireValkeyLock claims key for owner via SET NX PX, the same
// distributed-lock shape as channels.Registry.acquireLock. Returns false
// (not an error) when another replica already holds it.
func acquireValkeyLock(ctx context.Context, client valkey_go.Client, key, owner string, ttl time.Duration) (bool, error) {
	res := client.Do(ctx, client.B().Set().Key(key).Value(owner).Nx().PxMilliseconds(ttl.Milliseconds()).Build())
	str, err := res.ToString()
	if err != nil {
		if valkey_go.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return str == "OK", nil
}

// releaseValkeyLock deletes key only if its value still matches owner (a
// compare-and-delete Lua script), so a replica can never release a lock it
// no longer holds -- e.g. one it held past mintLeaseTTL that another replica
// has since re-acquired.
func releaseValkeyLock(ctx context.Context, client valkey_go.Client, key, owner string) error {
	const luaDel = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`
	return client.Do(ctx, client.B().Eval().Script(luaDel).Numkeys(1).Key(key).Arg(owner).Build()).Error()
}
