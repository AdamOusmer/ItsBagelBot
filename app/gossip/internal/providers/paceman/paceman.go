// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	sessionTTL     = 60 * time.Second
	nethersTTL     = 60 * time.Second
	lastFortTTL    = 60 * time.Second
	negativeTTL    = time.Minute
	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	personalBestTTL = 3 * time.Minute

	defaultHoursBetween = 6

	defaultRateLimit  = 120.0
	rateWindowSeconds = 60.0
)

type Config struct {
	BaseURL     string
	UserBaseURL string
	RateLimit   float64
}

const providerName = "paceman"

type api struct {
	http     *core.HTTPClient
	userHTTP *core.HTTPClient
	cache    *core.Cache
	log      *zap.Logger
	limiter  *ratelimit.Limiter
	buckets  core.Buckets
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	b.Endpoint("session").Timeout(handlerTimeout).Handle(p.session)
	b.Endpoint("nethers").Timeout(handlerTimeout).Handle(p.nethers)
	b.Endpoint("lastfort").Timeout(handlerTimeout).Handle(p.lastfort)
	b.Endpoint("personal_best").Timeout(handlerTimeout).Handle(p.personalBest)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
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
		http:     b.Client(base, nil, httpTimeout),
		userHTTP: b.Client(userBase, nil, httpTimeout),
		cache:    d.Cache,
		log:      d.Logger(),
		limiter:  d.Limiter,
		buckets:  core.NewBuckets("ratelimit:gossip:paceman", cfg.RateLimit, rateWindowSeconds),
	}
}

type splitStat struct {
	Count int    `json:"count"`
	Avg   string `json:"avg"`
}

type sessionStatsResponse struct {
	Nether          splitStat `json:"nether"`
	Bastion         splitStat `json:"bastion"`
	Fortress        splitStat `json:"fortress"`
	FirstStructure  splitStat `json:"first_structure"`
	SecondStructure splitStat `json:"second_structure"`
	FirstPortal     splitStat `json:"first_portal"`
	Stronghold      splitStat `json:"stronghold"`
	End             splitStat `json:"end"`
	Finish          splitStat `json:"finish"`
	Truncated       bool      `json:"truncated"`
}

type sessionNethersResponse struct {
	Count int     `json:"count"`
	Avg   string  `json:"avg"`
	RNPH  float64 `json:"rnph"`
}

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

type pbCompletion struct {
	Time int64 `json:"time"`
}

type userPBsResponse struct {
	PBs struct {
		Daily   *pbCompletion `json:"daily"`
		Weekly  *pbCompletion `json:"weekly"`
		Monthly *pbCompletion `json:"monthly"`
		AllTime *pbCompletion `json:"allTime"`
	} `json:"pbs"`
}

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

func (p *api) enforceRateLimit(ctx context.Context, isPremium bool) error {
	return p.buckets.Enforce(ctx, p.limiter, isPremium)
}

func (p *api) admit(isPremium bool) func(context.Context) error {
	return func(ctx context.Context) error { return p.enforceRateLimit(ctx, isPremium) }
}

func resolveHoursBetween(hoursBetween int) int {
	if hoursBetween <= 0 {
		return defaultHoursBetween
	}
	return hoursBetween
}

func cacheAccount(account string, hoursBetween int) string {
	return core.CacheID(account, strconv.Itoa(hoursBetween))
}

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

func (p *api) fetchUserPBs(ctx context.Context, account string) (userPBsResponse, error) {
	var resp userPBsResponse
	q := url.Values{"name": {account}, "sortByTime": {"1"}}
	if err := p.userHTTP.GetJSON(ctx, "/user", q, &resp); err != nil {
		return userPBsResponse{}, err
	}
	return resp, nil
}

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

func (p *api) cachedUserPBs(ctx context.Context, account string, isPremium bool) (userPBsResponse, error) {
	key := core.Key(providerName, "personal-best", cacheAccount(account, 0))
	return core.Cached(ctx, p.cache, key, personalBestTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (userPBsResponse, error) {
		return p.fetchUserPBs(ctx, account)
	})
}
