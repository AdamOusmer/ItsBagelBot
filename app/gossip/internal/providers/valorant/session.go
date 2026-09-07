// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

// warmCadence is how often a live broadcaster's session keeps rank and
// matches warm. Henrik caches both upstream endpoints for ~5min
// (rankTTL/matchesTTL say so, measured 2026-09-07); the fleet's HenrikDev key
// allows 30 req/min shared across every broadcaster and provider caller, and
// this loop is budgeted at 2 requests (rank + matches) per broadcaster per
// interval. Ticking faster than Henrik's own cache window would spend that
// shared budget re-fetching bytes Henrik hasn't changed yet, so this is
// pinned to the TTLs rather than a separate constant: a tick always lands
// just as the existing cache entry goes stale, never sooner.
const warmCadence = min(rankTTL, matchesTTL)

// warmEvery is the ticker period warmLoop actually uses; a var only so tests
// can run the loop at milliseconds instead of waiting five minutes per tick.
var warmEvery = time.Duration(warmCadence)

// warmTimeout bounds one warm tick's rank+matches refresh, mirroring
// handlerTimeout so a stalled upstream cannot wedge the loop goroutine.
const warmTimeout = handlerTimeout

// sessionTTL bounds how long a session key lives without a session_end.
// Twitch caps one broadcast at 48h, so a key older than that belongs to a
// stream whose stream.offline never reached us (sesame restart mid-stream,
// dropped EventSub delivery); letting it lapse is what stops a forgotten
// loop from spending 2 req/5min of the shared 30 req/min key forever.
const sessionTTL = 48 * time.Hour

// sessionKey is the Valkey marker that says "this channel is live and wants
// warming". It, not the in-memory map, is the source of truth: gossip runs 3
// replicas behind one NATS queue group, so the replica that receives
// session_end is usually NOT the one whose goroutine is ticking. The owning
// loop re-reads this key before every tick and stops when it is gone.
func sessionKey(channelID string) string { return core.Key(providerName, "session", channelID) }

// warmSession is one channel's running loop. Kept as a pointer so a loop
// that winds down can prove the map entry is still its own before deleting
// it, instead of clobbering a newer start that raced in after its end.
type warmSession struct{ cancel context.CancelFunc }

// sessions tracks the loops THIS replica owns, keyed by ChannelID.
// Package-scoped rather than a field on *api: there is exactly one valorant
// provider instance per gossip process, so a field would carry the same one
// map. Keeping the state here, instead of in valorant.go, is also what keeps
// this change out of the region another pass is editing there.
var sessions = struct {
	mu   sync.Mutex
	byID map[string]*warmSession
}{byID: map[string]*warmSession{}}

// sessionStart begins keeping the channel's linked Riot ID warm in the
// rank/matches cache for as long as the broadcaster streams. It validates the
// account the same way the rank/matches endpoints do (riotID), writes the
// session key, then starts a background loop that refreshes both through the
// exact same cache path the chat endpoints use: core.CachedBytes under
// core.Key("valorant", "rank"|"matches", id.Key), gated by the same p.budget
// admitter. The first refresh runs immediately, so the very first !valrank
// of the stream already reads a warm entry; every later tick lands as the
// previous entry goes stale. Idempotent on this replica: a channel already
// warming here just gets an ack. A duplicate start landing on a sibling
// replica spawns a second loop there, and the SWR claim in core.Cached
// collapses the two into one upstream call per window.
func (p *api) sessionStart(ctx context.Context, req gossiprpc.Request) any {
	if strings.TrimSpace(req.ChannelID) == "" {
		return gossiprpc.ValorantSessionReply{Error: "missing channel"}
	}
	id, reject := riotID(req)
	if reject != "" {
		return gossiprpc.ValorantSessionReply{Error: reject}
	}
	if err := p.cache.SetJSON(ctx, sessionKey(req.ChannelID), 1, sessionTTL); err != nil {
		return gossiprpc.ValorantSessionReply{Error: "session store unavailable"}
	}

	sessions.mu.Lock()
	if _, running := sessions.byID[req.ChannelID]; !running {
		wctx, cancel := context.WithCancel(context.Background())
		s := &warmSession{cancel: cancel}
		sessions.byID[req.ChannelID] = s
		go p.warmLoop(wctx, req, s)
	}
	sessions.mu.Unlock()

	return gossiprpc.ValorantSessionReply{Player: id.Display}
}

// sessionEnd stops the channel's warming. It deletes the session key (which
// is what reaches the loop when it lives on another replica) and cancels the
// local loop if this replica happens to own it. A channel with nothing
// running is a no-op ack, matching fortnite/mcsr's session_end idempotence.
// No upstream call, no budget spent.
func (p *api) sessionEnd(ctx context.Context, req gossiprpc.Request) any {
	if strings.TrimSpace(req.ChannelID) == "" {
		return gossiprpc.ValorantSessionReply{Error: "missing channel"}
	}
	_ = p.cache.DelJSON(ctx, sessionKey(req.ChannelID))
	sessions.mu.Lock()
	if s, ok := sessions.byID[req.ChannelID]; ok {
		s.cancel()
		delete(sessions.byID, req.ChannelID)
	}
	sessions.mu.Unlock()
	return gossiprpc.ValorantSessionReply{}
}

// forget drops the loop's own map entry once it winds down on its own (key
// gone). The pointer compare keeps it from evicting a newer loop that a
// fresh session_start installed under the same channel in the meantime.
func forget(channelID string, s *warmSession) {
	sessions.mu.Lock()
	if sessions.byID[channelID] == s {
		delete(sessions.byID, channelID)
	}
	sessions.mu.Unlock()
}

// sessionLive reports whether the channel's session key still exists. A
// Valkey read error counts as live: the outage is transient, the key cannot
// expire while Valkey is down, and stopping here would leave the rest of an
// hours-long stream cold because sesame only re-arms on the next
// stream.online.
func (p *api) sessionLive(ctx context.Context, channelID string) bool {
	found, err := p.cache.Exists(ctx, sessionKey(channelID))
	return err != nil || found
}

// warmLoop is the ticker sessionStart spawns, one per channel per replica.
// It refreshes once right away, then every warmCadence for as long as the
// session key exists, and exits when the key is gone, ctx is cancelled, or
// this replica exits — gossip's provider.Provider has no shutdown hook, so
// like every other in-memory piece of provider state a loop does not survive
// its own replica restarting; it resumes only when sesame's next
// stream.online resends session_start. warmRank and warmMatches swallow
// their own errors: a failed tick costs nothing a cold chat command would
// not already pay, and logging a routine upstream failure every five minutes
// for an hours-long stream is not worth the log volume.
func (p *api) warmLoop(ctx context.Context, req gossiprpc.Request, s *warmSession) {
	defer forget(req.ChannelID, s)
	t := time.NewTicker(warmEvery)
	defer t.Stop()
	for p.sessionLive(ctx, req.ChannelID) {
		p.warmTick(ctx, req)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// warmTick runs one refresh pass, bounded by warmTimeout so a stalled
// upstream cannot delay the next tick.
func (p *api) warmTick(ctx context.Context, req gossiprpc.Request) {
	wctx, cancel := context.WithTimeout(ctx, warmTimeout)
	defer cancel()
	p.warmRank(wctx, req)
	p.warmMatches(wctx, req)
}

// warmRank refreshes valorant.rank's cache entry for req through the same
// core.CachedBytes path the rank endpoint's flow builds: same key, same
// rankTTL/negativeTTL window, same p.budget admitter, same rankFetch — so a
// chat !valrank right after a tick reads exactly what this wrote, and a still
// -fresh entry costs no upstream call (admit only runs on a miss).
func (p *api) warmRank(ctx context.Context, req gossiprpc.Request) {
	id, reject := riotID(req)
	if reject != "" {
		return
	}
	_, _ = core.CachedBytes(ctx, p.cache, core.Key(providerName, "rank", id.Key),
		func(ctx context.Context) error { return p.budget(ctx, req) },
		func(ctx context.Context) ([]byte, time.Duration, error) {
			b, ttl, _, err := core.BuildReply(ctx, rankTTL, negativeTTL,
				func(ctx context.Context) (any, error) { return p.rankFetch(ctx, req, id) },
				func(msg string) any { return rankReply{Player: id.Display, Error: msg} },
			)
			return b, ttl, err
		})
}

// warmMatches is warmRank's twin for valorant.matches.
func (p *api) warmMatches(ctx context.Context, req gossiprpc.Request) {
	id, reject := riotID(req)
	if reject != "" {
		return
	}
	_, _ = core.CachedBytes(ctx, p.cache, core.Key(providerName, "matches", id.Key),
		func(ctx context.Context) error { return p.budget(ctx, req) },
		func(ctx context.Context) ([]byte, time.Duration, error) {
			b, ttl, _, err := core.BuildReply(ctx, matchesTTL, negativeTTL,
				func(ctx context.Context) (any, error) { return p.matchesFetch(ctx, req, id) },
				func(msg string) any { return matchesReply{Player: id.Display, Error: msg} },
			)
			return b, ttl, err
		})
}
