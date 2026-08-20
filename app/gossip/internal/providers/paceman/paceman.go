// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package paceman is the gossip provider for PaceMan.gg: a Minecraft
// speedrun pace-tracking site the community runs alongside MCSR Ranked. It
// answers sesame's !pace, !nethers and !lastfort commands, which stay on the
// mcsr module (a broadcaster's linked Minecraft account is the same account
// either way) even though the upstream and cache/rate-limit budget here are
// entirely independent of the mcsr provider.
//
// PaceMan's read endpoints need no key. They are rate-limited per client IP in
// a fixed 60-second window, so the budget here is keyed to the source address
// the fleet shares rather than to an account, and 429 carries Retry-After.
//
// This file holds the provider's wiring, upstream shapes and the
// fetch/cached HTTP layer. paceman_endpoints.go holds the four gossip
// endpoints (session, nethers, lastfort, personal_best) and the reply-shaping
// helpers built on top of this layer.
package paceman

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	// sessionTTL/nethersTTL/lastFortTTL: PaceMan's session window rolls
	// continuously (it is "last N hours", not a fixed period), so a run
	// finishing mid-minute should show up in chat within the minute, not
	// linger stale for the length of a run.
	sessionTTL     = 60 * time.Second
	nethersTTL     = 60 * time.Second
	lastFortTTL    = 60 * time.Second
	negativeTTL    = time.Minute
	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	// personalBestTTL: a player's daily/weekly/monthly/all-time PB only moves
	// on a discrete event (they finish a run faster than their standing best
	// for that window), the same "point value, not a rolling window" shape as
	// mcsr's lastMatchTTL — so it sits longer than the session TTLs above
	// (which track a continuously-rolling window and need to feel closer to
	// live) while still being short enough that a viewer asking right after a
	// new PB run sees it inside the same couple of minutes.
	personalBestTTL = 3 * time.Minute

	// defaultHoursBetween is the session-cutoff gap passed as hoursBetween
	// when a caller does not override it: long enough to survive a bathroom
	// break between runs, short enough that yesterday's runs never bleed into
	// today's session.
	defaultHoursBetween = 6

	// defaultRateLimit / rateWindowSeconds mirror PaceMan's published budget:
	// every route is limited per client IP in a fixed 60-second window, and the
	// routes this provider calls sit in the 180 (player stats) and 120 (cursor
	// histories) classes. The default takes the stricter of the two so one
	// endpoint's ceiling can never be spent by another's traffic.
	//
	// PaceMan counts per source IP where MCSR counts per API key, so the
	// fleet-wide Valkey bucket is deliberately stricter than the upstream:
	// several gossip pods can share one egress IP after masquerade, and a
	// per-pod budget would let them breach a limit none of them tracked.
	defaultRateLimit  = 120.0
	rateWindowSeconds = 60.0
)

// Config carries the provider's environment. PaceMan has no API key at all;
// BaseURL/RateLimit exist purely so an operator can point at a different host
// or tighten the budget without a redeploy. UserBaseURL is a second base: the
// /user personal-best lookup (paceman.personal_best) lives under
// paceman.gg/api/us, a different host path than the /stats/api split-tracking
// routes every other endpoint here calls, so it needs its own HTTP client
// pointed at its own base rather than a path glued onto BaseURL.
type Config struct {
	BaseURL     string
	UserBaseURL string
	RateLimit   float64
}

// providerName is the subject token this provider answers under.
const providerName = "paceman"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	http     *core.HTTPClient
	userHTTP *core.HTTPClient
	cache    *core.Cache
	log      *zap.Logger
	limiter  *ratelimit.Limiter
	buckets  core.Buckets
}

// New builds the paceman provider.
func New(cfg Config, d provider.Deps) provider.Provider {
	p := newAPI(cfg, d)
	b := provider.NewProvider(providerName, d)
	b.Endpoint("session").Timeout(handlerTimeout).Handle(p.session)
	b.Endpoint("nethers").Timeout(handlerTimeout).Handle(p.nethers)
	b.Endpoint("lastfort").Timeout(handlerTimeout).Handle(p.lastfort)
	b.Endpoint("personal_best").Timeout(handlerTimeout).Handle(p.personalBest)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://paceman.gg/stats/api"
	}
	userBase := strings.TrimSuffix(cfg.UserBaseURL, "/")
	if userBase == "" {
		userBase = "https://paceman.gg/api/us"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	return &api{
		http:     core.NewHTTPClient(base, nil, httpTimeout),
		userHTTP: core.NewHTTPClient(userBase, nil, httpTimeout),
		cache:    d.Cache,
		log:      d.Logger(),
		limiter:  d.Limiter,
		buckets:  core.NewBuckets("ratelimit:gossip:paceman", cfg.RateLimit, rateWindowSeconds),
	}
}

// --- upstream shapes -----------------------------------------------------------

// splitStat is one split's session count and pre-formatted "m:ss" average, the
// shape every field of getSessionStats shares.
type splitStat struct {
	Count int    `json:"count"`
	Avg   string `json:"avg"`
}

// sessionStatsResponse is the getSessionStats/ envelope: one splitStat per
// milestone plus whether the window got cut off by the result limit.
type sessionStatsResponse struct {
	Nether   splitStat `json:"nether"`
	Bastion  splitStat `json:"bastion"`
	Fortress splitStat `json:"fortress"`
	// FirstStructure/SecondStructure are the structure-order averages: whichever
	// of bastion/fortress the run entered first, and second. They are what a
	// runner compares against, since a bastion-first and a fortress-first run
	// are not the same split even at the same wall-clock time.
	FirstStructure  splitStat `json:"first_structure"`
	SecondStructure splitStat `json:"second_structure"`
	FirstPortal     splitStat `json:"first_portal"`
	Stronghold      splitStat `json:"stronghold"`
	End             splitStat `json:"end"`
	Finish          splitStat `json:"finish"`
	Truncated       bool      `json:"truncated"`
}

// sessionNethersResponse is the getSessionNethers/ envelope. RNPH is 0 for a
// player who submits runs manually instead of through the PaceMan Tracker —
// that is the site's own signal for "no live nethers-per-hour to show", not a
// zero result.
type sessionNethersResponse struct {
	Count int     `json:"count"`
	Avg   string  `json:"avg"`
	RNPH  float64 `json:"rnph"`
}

// recentTimestamp is one run's split-entry wall-clock timestamps (unix
// seconds, fractional) from getRecentTimestamps/. A split the run never
// reached decodes as a nil pointer, distinct from "reached at second zero".
type recentTimestamp struct {
	Start       float64  `json:"start"`
	Nether      *float64 `json:"nether"`
	Bastion     *float64 `json:"bastion"`
	Fortress    *float64 `json:"fortress"`
	FirstPortal *float64 `json:"first_portal"`
	Stronghold  *float64 `json:"stronghold"`
	End         *float64 `json:"end"`
	Finish      *float64 `json:"finish"`
}

// pbCompletion is one personal-best entry from the /user envelope's pbs
// object: PaceMan precomputes the milliseconds already, gossip only formats
// it. A nil pbCompletion (the JSON field is `null`) means the player has no
// best in that window yet, distinct from a present-but-zero time.
type pbCompletion struct {
	Time int64 `json:"time"`
}

// userPBsResponse is the /user?name=&sortByTime=1 envelope subset gossip
// reads: this single call answers all four PB windows at once (see
// fetchUserPBs), so !pb never pays a second round trip for daily vs. weekly
// vs. monthly vs. all-time — only the reply shaping picks which field to
// read.
type userPBsResponse struct {
	PBs struct {
		Daily   *pbCompletion `json:"daily"`
		Weekly  *pbCompletion `json:"weekly"`
		Monthly *pbCompletion `json:"monthly"`
		AllTime *pbCompletion `json:"allTime"`
	} `json:"pbs"`
}

// friendlyError maps an upstream failure onto a user-facing reply error, or
// returns "" for an infrastructure failure. PaceMan answers a plain 4xx for a
// name it cannot resolve, and answers 429 with {"error":"Too many requests"}
// plus Retry-After once the per-IP window is spent.
func friendlyError(err error) string {
	var ue *core.UpstreamError
	if !errors.As(err, &ue) {
		return ""
	}
	switch {
	case ue.Status == 429:
		return "PaceMan is busy, try again in a minute"
	case ue.Status >= 400 && ue.Status < 500:
		return "player not found"
	}
	return ""
}

// enforceRateLimit consumes one request from the PaceMan budget under the
// shared premium/standard bucket discipline (see core.Buckets).
func (p *api) enforceRateLimit(ctx context.Context, isPremium bool) error {
	return p.buckets.Enforce(ctx, p.limiter, isPremium)
}

// admit binds the budget to one lane for a cached lookup; see mcsr's admit
// doc for why the check lives here and not inside the fill closure.
func (p *api) admit(isPremium bool) func(context.Context) error {
	return func(ctx context.Context) error { return p.enforceRateLimit(ctx, isPremium) }
}

// resolveHoursBetween applies the shared default when a caller leaves the
// session-cutoff gap unset.
func resolveHoursBetween(hoursBetween int) int {
	if hoursBetween <= 0 {
		return defaultHoursBetween
	}
	return hoursBetween
}

// cacheAccount builds the cache-key id for an account-scoped lookup, folding
// in whatever else changes the answer (the session-cutoff gap) so two
// different windows never collide on one entry.
func cacheAccount(account string, hoursBetween int) string {
	return strings.ToLower(strings.TrimSpace(account)) + ":" + strconv.Itoa(hoursBetween)
}

// --- upstream fetches ------------------------------------------------------------

// sessionQuery bundles the account and session-cutoff gap threaded through
// the session-stats/session-nethers fetch+cache pairs, so their signatures
// carry one named value instead of the same (account string, hoursBetween
// int) pair repeated across all four functions.
type sessionQuery struct {
	Account      string
	HoursBetween int
}

func (p *api) fetchSessionStats(ctx context.Context, q sessionQuery) (sessionStatsResponse, error) {
	var resp sessionStatsResponse
	vals := url.Values{"name": {q.Account}, "hoursBetween": {strconv.Itoa(q.HoursBetween)}}
	if err := p.http.GetJSON(ctx, "/getSessionStats/", vals, &resp); err != nil {
		return sessionStatsResponse{}, err
	}
	return resp, nil
}

func (p *api) fetchSessionNethers(ctx context.Context, q sessionQuery) (sessionNethersResponse, error) {
	var resp sessionNethersResponse
	vals := url.Values{"name": {q.Account}, "hoursBetween": {strconv.Itoa(q.HoursBetween)}, "dp": {"0"}}
	if err := p.http.GetJSON(ctx, "/getSessionNethers/", vals, &resp); err != nil {
		return sessionNethersResponse{}, err
	}
	return resp, nil
}

func (p *api) fetchLastFort(ctx context.Context, account string) ([]recentTimestamp, error) {
	var resp []recentTimestamp
	q := url.Values{"name": {account}, "limit": {"1"}, "onlyFort": {"true"}}
	if err := p.http.GetJSON(ctx, "/getRecentTimestamps/", q, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// fetchUserPBs loads all four of a player's precomputed personal bests in one
// call. sortByTime=1 matches the site's own leaderboard ordering; it has no
// bearing on this single-player lookup but is included so the request mirrors
// what PaceMan's own client sends, in case an unordered variant of the
// endpoint is ever rate-limited differently. Uses userHTTP (api/us), not the
// http client every other fetch in this file uses (stats/api) — see Config.
func (p *api) fetchUserPBs(ctx context.Context, account string) (userPBsResponse, error) {
	var resp userPBsResponse
	q := url.Values{"name": {account}, "sortByTime": {"1"}}
	if err := p.userHTTP.GetJSON(ctx, "/user", q, &resp); err != nil {
		return userPBsResponse{}, err
	}
	return resp, nil
}

// --- cached fetches --------------------------------------------------------------

func (p *api) cachedSessionStats(ctx context.Context, q sessionQuery, isPremium bool) (sessionStatsResponse, error) {
	key := core.Key(providerName, "session-stats", cacheAccount(q.Account, q.HoursBetween))
	return core.Cached(ctx, p.cache, key, sessionTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (sessionStatsResponse, error) {
		return p.fetchSessionStats(ctx, q)
	})
}

func (p *api) cachedSessionNethers(ctx context.Context, q sessionQuery, isPremium bool) (sessionNethersResponse, error) {
	key := core.Key(providerName, "session-nethers", cacheAccount(q.Account, q.HoursBetween))
	return core.Cached(ctx, p.cache, key, nethersTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (sessionNethersResponse, error) {
		return p.fetchSessionNethers(ctx, q)
	})
}

func (p *api) cachedLastFort(ctx context.Context, account string, isPremium bool) ([]recentTimestamp, error) {
	key := core.Key(providerName, "lastfort", cacheAccount(account, 0))
	return core.Cached(ctx, p.cache, key, lastFortTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) ([]recentTimestamp, error) {
		return p.fetchLastFort(ctx, account)
	})
}

// cachedUserPBs caches the whole four-window pbs object under one key per
// account: !pb daily and !pb weekly for the same player hit the same cache
// entry, so asking about a second window right after the first costs nothing
// extra.
func (p *api) cachedUserPBs(ctx context.Context, account string, isPremium bool) (userPBsResponse, error) {
	key := core.Key(providerName, "personal-best", cacheAccount(account, 0))
	return core.Cached(ctx, p.cache, key, personalBestTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (userPBsResponse, error) {
		return p.fetchUserPBs(ctx, account)
	})
}
