// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"

	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// Guard buckets are small self-standing per-subject caps that sit ABOVE the
// real platform ceilings, and they are deliberately NOT leased from the
// LeaseManager even though it also implements ratelimit.Manager.
//
// WHY NOT THE LEASE MANAGER: leases carve each bucket into per-pod shares so
// N replicas cannot overspend a Twitch ceiling — but the coordinator's
// emergency-partition rewrite is allowed to SHRINK or drop any bucket's plan
// to keep the core chat lanes admitting during an incident. A guard that rides
// the lease plan silently shrinks with it: exactly when the fleet is degraded
// and someone is flooding announce/shoutout, the guard would loosen instead of
// holding its number. Guards therefore go through a direct central Limiter
// (ratelimit.New) straight against Valkey, where their spec is process-constant
// and nothing can rewrite it. The cost — every replica hits Valkey for these
// rare calls instead of a local lease — is paid on paths measured in single
// calls per channel per minute, never on the chat send path.
const (
	// AnnounceGuardPerMin = 10/min ≈ 2× Twitch's ~5 announcements-per-minute
	// platform ceiling: legitimate volume always passes the guard and gets its
	// verdict from Twitch, while floods (the thing that risks the bot account)
	// are cut locally at 2× before they ever reach Helix.
	announceGuardPerMin = 10.0
	// ShoutoutGuardPer300s = 4 per 300s ≈ 2× Twitch's ~2-shoutouts-per-10-minutes
	// ceiling. Same shape as announce: platform rejects first in normal use;
	// the guard exists so an announce/shoutout loop cannot hammer Helix or spam
	// viewer-facing notifications while it does.
	shoutoutGuardPer300s = 4.0
	// LookupGuardPerMin = 30/min per broadcaster (and per target for accountage):
	// one loyalty tick + followage/accountage command traffic per channel sits
	// orders of magnitude below this; only a runaway producer or scripted RPC
	// caller trips it. TTL comes from NewSpec as ceil(cap/rate)*2 = 120s, i.e.
	// two full windows must pass idle before the key evaporates, so a bursty
	// caller cannot ride an expired key to double spend.
	lookupGuardPerMin = 30.0
)

// Guard specs are built once: NewSpec pre-formats the float/TTL Lua arguments,
// and these requests are shared values safe to reuse across goroutines.
var (
	announceGuardSpec = ratelimit.NewSpec(announceGuardPerMin, announceGuardPerMin/60.0)
	shoutoutGuardSpec = ratelimit.NewSpec(shoutoutGuardPer300s, shoutoutGuardPer300s/300.0)
	lookupGuardSpec   = ratelimit.NewSpec(lookupGuardPerMin, lookupGuardPerMin/60.0)
)

// The exported request builders pin the exact lane bucket keys/specs for
// callers outside this package (the management RPC handlers), so the RPC side
// pays the SAME partitions the lanes pay instead of a drifted near-copy:
// ratelimit:helix:app holds the 700/min general app partition and
// ratelimit:helix:user:bot the bot identity's 800/min user budget (both see
// buckets.go for how they partition the real 800/min Helix limit).

// HelixAppPartition returns the shared app-token partition request.
func HelixAppPartition() ratelimit.Request {
	return helixGeneralSpec.ForKey("ratelimit:helix:app")
}

// HelixBotUserPartition returns the bot user-token partition request.
func HelixBotUserPartition() ratelimit.Request {
	return helixUserSpec.ForKey("ratelimit:helix:user:bot")
}

// LookupGuard returns the per-broadcaster lookup guard request (followage,
// chatters).
func LookupGuard(broadcasterID string) ratelimit.Request {
	return lookupGuardSpec.ForDynamicKey("ratelimit:lookup:", "lookup", broadcasterID)
}

// LookupTargetGuard returns the per-target lookup guard request (accountage),
// keyed by the looked-up id rather than the asking channel so one hot target
// cannot drain every asker's budget.
func LookupTargetGuard(targetID string) ratelimit.Request {
	return lookupGuardSpec.ForDynamicKey("ratelimit:lookup:target:", "lookup:target", targetID)
}

// AnnounceGuard returns the per-channel announce guard request.
func AnnounceGuard(broadcasterID string) ratelimit.Request {
	return announceGuardSpec.ForDynamicKey("ratelimit:announce:", "announce", broadcasterID)
}

// ShoutoutGuard returns the per-channel shoutout guard request.
func ShoutoutGuard(broadcasterID string) ratelimit.Request {
	return shoutoutGuardSpec.ForDynamicKey("ratelimit:shoutout:", "shoutout", broadcasterID)
}

// SetGuardLimiter attaches the direct central limiter backing the guard
// buckets. Wiring calls it once per worker; nil (unset) disables guarding and
// every allow* helper fails open.
func (w *Worker) SetGuardLimiter(m ratelimit.Manager) { w.guards = m }

// AllowFailOpen consumes one token from req via m and reports admission.
//
// Fail-open contract, deliberate twice over: a nil manager (tests, minimal
// deployments) and a Valkey error both admit. The guards cap ABUSE of paths
// that worked unthrottled before; letting a cache blip turn followage,
// accountage, chatters, announce or shoutout into hard outages would trade a
// bounded abuse window for user-visible breakage on every Valkey restart. The
// invisible-quota bug these guards close was silent underuse of quota, not an
// outage — the fix must not introduce one.
func AllowFailOpen(ctx context.Context, m ratelimit.Manager, req ratelimit.Request, log *zap.Logger) bool {
	if m == nil {
		return true
	}
	ok, err := m.Allow(ctx, req)
	if err != nil {
		log.Warn("guard bucket unavailable; failing open",
			zap.String("bucket", guardKeyName(req)), zap.Error(err))
		return true
	}
	return ok
}

// guardKeyName renders a request's Valkey key for logs (Allow itself does the
// deferred concatenation internally).
func guardKeyName(req ratelimit.Request) string {
	if req.Key != "" {
		return req.Key
	}
	return req.DynamicPrefix + req.Bucket.Value
}

// takeAnnouncePerChannel consumes the per-channel announce guard before the
// shared Helix take in processAnnounce.
func (w *Worker) takeAnnouncePerChannel(ctx context.Context, broadcasterID string) bool {
	return AllowFailOpen(ctx, w.guards, AnnounceGuard(broadcasterID), w.log)
}

// takeShoutoutPerChannel consumes the per-channel shoutout guard before target
// resolution and the shared Helix take in processShoutout.
func (w *Worker) takeShoutoutPerChannel(ctx context.Context, broadcasterID string) bool {
	return AllowFailOpen(ctx, w.guards, ShoutoutGuard(broadcasterID), w.log)
}
