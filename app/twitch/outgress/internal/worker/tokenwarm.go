// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// tokenWarmSlots bounds the in-flight background token pre-warms per lane
// worker (see the tokenWarm field). Go-lives fan out at most one warm per
// broadcaster, and Twitch's own per-token 800/min Helix budget (guarded by
// takeSystemHelix below) already caps how fast these can drain, so this only
// trips under an abnormal simultaneous-go-live burst — and tripping merely
// skips the pre-warm for the excess, never a real chat/mod/ad send.
//
// tokenWarmTimeout bounds one whole background warm (token source refresh +
// the /helix/users probe read). Most warms now adopt an already-valid access
// token from the users service instead of minting one (see
// warmBroadcasterToken and twitch.NewStoredUserTokenSource's adoption path):
// that path is a NATS RPC plus a MySQL read, ~8ms. This bound stays sized for
// the slow case -- a real Twitch mint, ~320ms cold (measured 2026-08-20: 80.5ms
// TCP RTT to id.twitch.tv x roughly 3 round trips) -- because nothing waits
// on this call; the bound exists only to stop a stuck one from holding a
// slot forever, not to keep the common case fast.
const (
	tokenWarmSlots   = 16
	tokenWarmTimeout = 10 * time.Second
)

// SubscribeTokenWarm binds this replica to the core-NATS (non-queue)
// token-warm fan-out the projector publishes on go-live (see the projector's
// warmBroadcasterToken and outgress.TokenWarmScope). Every outgress replica
// calls this once at startup on its own *nats.Conn, so every replica gets its
// own subscription and therefore every published warm — the exact fan-out
// shape channels.Registry already uses for its cache-invalidation listeners,
// as opposed to a queue-grouped lane subject where only one replica in the
// group would ever see a given message.
//
// WHY FAN-OUT INSTEAD OF THE OUTGRESS SYSTEM LANE: this used to publish a
// TypeWarmToken job onto the queue-grouped outgress system lane (one durable
// consumer shared by all 3 replicas, "outgress-system"). That structurally
// warms exactly one replica's twitch.BroadcasterTokens cache; the other two
// still pay the full cold mint on their own first "as":"broadcaster" send for
// that channel. A per-replica in-memory cache needs a per-replica warm, which
// only a fan-out transport can deliver — the lane's queue grouping was the
// wrong tool for this regardless of tuning.
//
// Caller owns the returned subscription's lifetime (Unsubscribe on shutdown),
// matching how channels.Registry.Close tears down its own invalidation subs.
func (w *Worker) SubscribeTokenWarm(nc *nats.Conn, prefix string) (*nats.Subscription, error) {
	return nc.Subscribe(prefix+"."+outgress.TokenWarmScope, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &dto); err != nil {
			w.log.Debug("token-warm: bad payload", zap.Error(err))
			return
		}
		if dto.BroadcasterID == "" {
			return
		}
		w.scheduleTokenWarm(dto.BroadcasterID)
	})
}

// scheduleTokenWarm runs the warm detached from the NATS dispatch callback
// (even the common ~8ms store-adoption path, let alone a ~320ms cold mint —
// see warmBroadcasterToken) so it never delays delivery of the next fan-out
// message on this subscription. Slot-bounded by tokenWarm; see its field doc.
func (w *Worker) scheduleTokenWarm(broadcasterID string) {
	select {
	case w.tokenWarm <- struct{}{}:
	default:
		w.log.Warn("token-warm skipped: checks saturated", zap.String("broadcaster_id", broadcasterID))
		return
	}
	go func() {
		defer func() { <-w.tokenWarm }()
		ctx, cancel := context.WithTimeout(context.Background(), tokenWarmTimeout)
		defer cancel()
		w.warmBroadcasterToken(ctx, broadcasterID)
	}()
}

// warmBroadcasterToken forces this replica's broadcaster token cache
// (twitch.BroadcasterTokens) to hold a valid access token for broadcasterID,
// so it is already hot by the time real automated traffic (chat greetings,
// mod actions, ads) needs it. "Forces" no longer means "always mints": the
// underlying Source (twitch.NewStoredUserTokenSource) adopts an already-valid
// access token from the users service store when one exists, and only mints
// a new one from the refresh token when it doesn't. See that function's doc
// for the adoption path itself; this comment covers what changes for the
// fan-out warm specifically.
//
// Numbers (measured 2026-08-20): id.twitch.tv is 80.5ms TCP RTT from a
// cluster pod, and a cold TLS handshake plus token request costs ~320ms.
// That gap is almost the entire difference between a channel's first chat
// send (~360-390ms) and every send after (~15-25ms) — see
// twitch.Client.Warmup, which mints the shared app/bot tokens at boot for
// the same reason. A broadcaster's own user token is never covered by that
// boot warmup: it is loaded lazily, per channel, on first "as":"broadcaster"
// use, and each of outgress's 3 replicas keeps an INDEPENDENT in-memory
// cache -- but with adoption, only the FIRST replica to need a given
// broadcaster's token (after a deploy, an LRU eviction, or the token's own
// expiry) pays the ~320ms mint. The other two replicas' warms (and this
// fan-out delivers one warm to all 3 on every go-live) read the same stored
// access token the first one just wrote and adopt it, ~8ms: one NATS RPC to
// the users service plus one MySQL read, no request to Twitch at all.
//
// GET /helix/users with no id/login returns the token's own account and
// needs no scope beyond a valid user token, so it is the cheapest read that
// exercises the token source (the same trick twitch.Client.warmupBotToken
// uses for the bot's own token) while also warming a reusable connection to
// Twitch on the replica that does end up minting.
//
// Routed through callTwitch (not twitch.Client directly) so a dead
// broadcaster grant discovered here also updates the grant-health marker,
// same as every other broadcaster-identity call.
//
// FORMER CONCURRENT-MINT RACE (fixed by adoption, kept here for the next
// person who finds this comment while chasing a grant-health flap): before
// adoption existed, this fan-out meant all 3 replicas minted simultaneously
// on every go-live, each POSTing the SAME stored refresh token to
// id.twitch.tv within milliseconds of the other two. Twitch ROTATES the
// refresh token on redemption, so at most one of those 3 POSTs would win;
// the losers would persist (or keep in memory) a refresh token Twitch had
// already invalidated -- this repo's known "broadcaster grant dies silently"
// failure mode. twitch.Source.singleflightRefresh only ever collapsed
// concurrent refreshes WITHIN one replica's process, never across the 3, and
// takeSystemHelix below only guards this fleet's own Helix call budget, not
// the OAuth POST to id.twitch.tv. Adoption removes the race at its root: a
// replica that finds a valid stored access token never redeems the refresh
// token at all, so there is nothing left to collide over. If a grant-health
// flap (noteGrantHealth marking the grant dead) is ever seen right after a
// go-live again, this is the history to know about -- but it should now
// require an actual Twitch-side revocation, not 3 replicas racing each other.
//
// Best-effort throughout: this optimizes latency only. A failure here costs
// nothing but the pre-warm status quo (the next real caller pays whatever
// this warm would have paid -- adoption or mint -- instead), so it never
// returns an error and never nacks or retries.
func (w *Worker) warmBroadcasterToken(ctx context.Context, broadcasterID string) {
	if err := w.takeSystemHelix(ctx); err != nil {
		w.log.Warn("token-warm: no system budget, skipping",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}

	res, err := w.callTwitch(ctx, twitch.IdentityBroadcaster, broadcasterID,
		twitch.HelixCall{Method: http.MethodGet, Endpoint: "/helix/users"})
	if err != nil {
		w.log.Warn("token-warm: broadcaster token unavailable",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	drainResponse(res)

	// "warmed" rather than "minted": this call may have adopted an already-
	// valid stored access token instead of minting a new one -- see this
	// function's doc. The Source doesn't report which path it took, and
	// nothing here needs to know; either way the cache is hot now.
	w.log.Debug("token-warm: broadcaster token warmed",
		zap.String("broadcaster_id", broadcasterID))
}
