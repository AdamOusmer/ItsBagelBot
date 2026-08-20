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
package paceman

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/monitor"
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
// or tighten the budget without a redeploy.
type Config struct {
	BaseURL   string
	RateLimit float64
}

// providerName is the subject token this provider answers under.
const providerName = "paceman"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	log     *zap.Logger
	limiter *ratelimit.Limiter
	buckets core.Buckets
}

// New builds the paceman provider.
func New(cfg Config, d provider.Deps) provider.Provider {
	p := newAPI(cfg, d)
	b := provider.NewProvider(providerName, d)
	b.Endpoint("session").Timeout(handlerTimeout).Handle(p.session)
	b.Endpoint("nethers").Timeout(handlerTimeout).Handle(p.nethers)
	b.Endpoint("lastfort").Timeout(handlerTimeout).Handle(p.lastfort)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://paceman.gg/stats/api"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	return &api{
		http:    core.NewHTTPClient(base, nil, httpTimeout),
		cache:   d.Cache,
		log:     d.Logger(),
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:paceman", cfg.RateLimit, rateWindowSeconds),
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

func (p *api) fetchSessionStats(ctx context.Context, account string, hoursBetween int) (sessionStatsResponse, error) {
	var resp sessionStatsResponse
	q := url.Values{"name": {account}, "hoursBetween": {strconv.Itoa(hoursBetween)}}
	if err := p.http.GetJSON(ctx, "/getSessionStats/", q, &resp); err != nil {
		return sessionStatsResponse{}, err
	}
	return resp, nil
}

func (p *api) fetchSessionNethers(ctx context.Context, account string, hoursBetween int) (sessionNethersResponse, error) {
	var resp sessionNethersResponse
	q := url.Values{"name": {account}, "hoursBetween": {strconv.Itoa(hoursBetween)}, "dp": {"0"}}
	if err := p.http.GetJSON(ctx, "/getSessionNethers/", q, &resp); err != nil {
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

// --- cached fetches --------------------------------------------------------------

func (p *api) cachedSessionStats(ctx context.Context, account string, hoursBetween int, isPremium bool) (sessionStatsResponse, error) {
	key := core.Key(providerName, "session-stats", cacheAccount(account, hoursBetween))
	return core.Cached(ctx, p.cache, key, sessionTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (sessionStatsResponse, error) {
		return p.fetchSessionStats(ctx, account, hoursBetween)
	})
}

func (p *api) cachedSessionNethers(ctx context.Context, account string, hoursBetween int, isPremium bool) (sessionNethersResponse, error) {
	key := core.Key(providerName, "session-nethers", cacheAccount(account, hoursBetween))
	return core.Cached(ctx, p.cache, key, nethersTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (sessionNethersResponse, error) {
		return p.fetchSessionNethers(ctx, account, hoursBetween)
	})
}

func (p *api) cachedLastFort(ctx context.Context, account string, isPremium bool) ([]recentTimestamp, error) {
	key := core.Key(providerName, "lastfort", cacheAccount(account, 0))
	return core.Cached(ctx, p.cache, key, lastFortTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) ([]recentTimestamp, error) {
		return p.fetchLastFort(ctx, account)
	})
}

// --- reply shaping -----------------------------------------------------------------

// buildSessionReply combines the two session upstream calls into one reply.
// Empty tracks NetherCount alone: a run cannot reach any later split without
// first entering a nether, so a zero nether count is the one check that means
// "nothing tracked this window" for every field below it.
func buildSessionReply(account string, stats sessionStatsResponse, nethers sessionNethersResponse) gossiprpc.PacemanSessionReply {
	return gossiprpc.PacemanSessionReply{
		Player:          account,
		NetherCount:     stats.Nether.Count,
		Nether:          stats.Nether.Avg,
		Bastion:         stats.Bastion.Avg,
		Fortress:        stats.Fortress.Avg,
		FirstStructure:  stats.FirstStructure.Avg,
		SecondStructure: stats.SecondStructure.Avg,
		FirstPortal:     stats.FirstPortal.Avg,
		Stronghold:      stats.Stronghold.Avg,
		End:             stats.End.Avg,
		Finish:          stats.Finish.Avg,
		NPH:             nethers.RNPH,
		Empty:           stats.Nether.Count == 0,
	}
}

// unixTime converts a PaceMan fractional-second epoch value to a time.Time at
// whole-second resolution; sub-second precision does not change either the
// "m:ss" split rendering or the "how long ago" rounding these replies do.
func unixTime(sec float64) time.Time { return time.Unix(int64(sec), 0) }

// formatMMSS renders an elapsed duration the way PaceMan's own "avg" fields
// do ("m:ss"), so a computed split reads identically to an upstream one.
func formatMMSS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int64(seconds)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// splitDuration renders one run split as elapsed time since the run started,
// or "" when the run never reached it (module.go turns that into an em dash).
func splitDuration(start float64, split *float64) string {
	if split == nil {
		return ""
	}
	return formatMMSS(*split - start)
}

// buildLastFortReply shapes one recentTimestamp row into the reply, deriving
// each split's elapsed time from its wall-clock timestamp and the run's start.
func buildLastFortReply(account string, run recentTimestamp) gossiprpc.PacemanLastFortReply {
	return gossiprpc.PacemanLastFortReply{
		Player:      account,
		Nether:      splitDuration(run.Start, run.Nether),
		Bastion:     splitDuration(run.Start, run.Bastion),
		Fortress:    splitDuration(run.Start, run.Fortress),
		FirstPortal: splitDuration(run.Start, run.FirstPortal),
		Stronghold:  splitDuration(run.Start, run.Stronghold),
		End:         splitDuration(run.Start, run.End),
		Finish:      splitDuration(run.Start, run.Finish),
		AgoSeconds:  int64(time.Since(unixTime(run.Start)).Seconds()),
	}
}

// --- endpoints ------------------------------------------------------------------

func (p *api) session(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.PacemanSessionReply{Error: "missing account"}
	}
	hoursBetween := resolveHoursBetween(req.HoursBetween)

	stats, err := p.cachedSessionStats(ctx, account, hoursBetween, req.IsPremium)
	if err != nil {
		return sessionErrorReply(log, account, err)
	}
	nethers, err := p.cachedSessionNethers(ctx, account, hoursBetween, req.IsPremium)
	if err != nil {
		return sessionErrorReply(log, account, err)
	}
	return buildSessionReply(account, stats, nethers)
}

// sessionErrorReply maps a fetch failure to a paceman.session reply: a
// friendly hit (upstream 4xx/429) stays quiet in the logs since it is normal
// upstream behavior, anything else logs a warning and answers a generic
// message so the viewer still gets a line instead of silence.
func sessionErrorReply(log *zap.Logger, account string, err error) gossiprpc.PacemanSessionReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman session fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanSessionReply{Player: account, Error: msg}
}

func (p *api) nethers(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.PacemanNethersReply{Error: "missing account"}
	}
	hoursBetween := resolveHoursBetween(req.HoursBetween)

	nethers, err := p.cachedSessionNethers(ctx, account, hoursBetween, req.IsPremium)
	if err != nil {
		return nethersErrorReply(log, account, err)
	}
	return gossiprpc.PacemanNethersReply{
		Player: account,
		Count:  nethers.Count,
		Avg:    nethers.Avg,
		NPH:    nethers.RNPH,
		Empty:  nethers.Count == 0,
	}
}

func nethersErrorReply(log *zap.Logger, account string, err error) gossiprpc.PacemanNethersReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman nethers fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanNethersReply{Player: account, Error: msg}
}

func (p *api) lastfort(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.PacemanLastFortReply{Error: "missing account"}
	}

	runs, err := p.cachedLastFort(ctx, account, req.IsPremium)
	if err != nil {
		return lastFortErrorReply(log, account, err)
	}
	if len(runs) == 0 {
		return gossiprpc.PacemanLastFortReply{Player: account, Empty: true}
	}
	return buildLastFortReply(account, runs[0])
}

func lastFortErrorReply(log *zap.Logger, account string, err error) gossiprpc.PacemanLastFortReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("paceman lastfort fetch failed", zap.String("account", account), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.PacemanLastFortReply{Player: account, Error: msg}
}
