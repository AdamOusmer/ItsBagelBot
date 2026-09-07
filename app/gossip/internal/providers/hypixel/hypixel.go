// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package hypixel is the gossip provider for the direct Hypixel API: lifetime
// Bed Wars stats for sesame's !bwstats. It is its own provider — not a path
// inside urchin — because it is a separate external system with its own key,
// its own (much smaller) rate budget and its own failure modes; on the
// dashboard the command still lives on the one urchin module page, which is a
// sesame/console concern, not a gossip one.
//
// The player identifier resolves through Mojang's public profile endpoint, so
// this provider depends on no other provider. Stats is byte-flow: the reply is
// shaped and marshaled once on fetch, and a cache hit answers with the stored
// wire bytes untouched. hypixel.uuid is a Handle: it reuses the same Mojang
// cache stats already fills, and stays up even when the Hypixel key is unset
// so the dashboard can persist a linked-account uuid without !bwstats.
package hypixel

import (
	"context"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	// statsTTL matches urchin's lifetime-stats staleness budget.
	statsTTL    = 10 * time.Minute
	negativeTTL = 5 * time.Minute
	// uuidTTL: a Mojang name→uuid binding only changes on a rename.
	uuidTTL = 24 * time.Hour

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	// rateWindowSeconds is the Hypixel key budget window (5 minutes).
	rateWindowSeconds = 300.0
	// mojangWindowSeconds is Mojang's profile-endpoint budget window (10
	// minutes). It is a separate window from Hypixel's because it is a separate
	// upstream: Mojang throttles per source IP, Hypixel per key.
	mojangWindowSeconds = 600.0
)

// Config carries the provider's environment. APIKey empty = stats endpoint
// omitted (uuid resolve still runs; Mojang needs no Hypixel key). RateLimit is
// requests per 5 minutes; Hypixel personal keys allow 300.
type Config struct {
	BaseURL       string
	MojangBaseURL string
	APIKey        string
	RateLimit     float64
	// MojangRateLimit is the Mojang profile endpoint's own budget per
	// mojangWindowSeconds. It is separate from RateLimit: the name resolve and
	// the stats call hit different systems with different throttles, and the
	// resolve happens first, so metering only the Hypixel call leaves Mojang
	// completely unguarded.
	MojangRateLimit float64
}

// providerName is the subject token this provider answers under.
const providerName = "hypixel"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	http    *core.HTTPClient
	mojang  *core.HTTPClient
	cache   *core.Cache
	log     *zap.Logger
	limiter *ratelimit.Limiter
	buckets core.Buckets
	// mojangBuckets is the resolve hop's own budget. Sharing `buckets` would be
	// wrong twice over: it would spend the Hypixel key's allowance on calls that
	// never reach Hypixel, and it would bound Mojang by a window Mojang does not
	// use.
	mojangBuckets core.Buckets
}

// New builds the hypixel provider: Mojang uuid resolve always, and the
// byte-flow stats endpoint when an API key is configured.
//
// Trusted is declared before any client exists — trust is positional — and
// marks both dialing surfaces (Hypixel, Mojang) direct-egress.
func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	b.Endpoint("uuid").Timeout(handlerTimeout).Handle(p.uuid)
	if cfg.APIKey != "" {
		b.Endpoint("stats").Timeout(handlerTimeout).
			Cached(statsTTL, negativeTTL).
			Reply(statsErrReply).
			Budget(p.statsBudget).
			Fallback("stats lookup failed").
			Fetch(p.statsFetch)
	}
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.hypixel.net"
	}
	mojangBase := strings.TrimSuffix(cfg.MojangBaseURL, "/")
	if mojangBase == "" {
		mojangBase = "https://api.mojang.com"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 300
	}
	if cfg.MojangRateLimit <= 0 {
		cfg.MojangRateLimit = 600
	}
	return &api{
		http:          b.Client(base, map[string]string{"API-Key": cfg.APIKey}, httpTimeout),
		mojang:        b.Client(mojangBase, nil, httpTimeout),
		cache:         d.Cache,
		log:           d.Logger(),
		limiter:       d.Limiter,
		buckets:       core.NewBuckets("ratelimit:gossip:hypixel", cfg.RateLimit, rateWindowSeconds),
		mojangBuckets: core.NewBuckets("ratelimit:gossip:mojang", cfg.MojangRateLimit, mojangWindowSeconds),
	}
}

// statsErrReply shapes every stats error reply (missing account, friendly
// upstream failure, infrastructure fallback).
func statsErrReply(id, msg string) any {
	return gossiprpc.HypixelStatsReply{Player: id, Error: msg}
}

// statsFetch adapts fetchStats to the flow's fetch signature.
func (p *api) statsFetch(ctx context.Context, _ gossiprpc.Request, id provider.ID) (any, error) {
	return p.fetchStats(ctx, id.Display)
}

// statsBudget spends one request's share of BOTH upstreams a cold stats lookup
// touches, in that request's own lane. Mojang and Hypixel are separate services
// with separate allowances and separate buckets, so each is debited in turn: a
// resolve that exhausts Mojang's per-IP allowance must be visible as such, not
// hidden behind a Hypixel key that still looks unspent.
//
// It is declared on the endpoint rather than written inside the fetch because a
// fetch runs once per singleflight flight. A check in there is charged to
// whichever caller won the flight and its verdict is served to everyone joined
// to it, which is how a drained standard bucket denied premium callers the
// reserve they are entitled to.
//
// An account given as a raw uuid needs no Mojang call, so it skips that
// bucket: linked-account configs store the uuid precisely so this hop is
// gone, and charging it would spend Mojang's per-IP allowance on traffic
// that never reaches Mojang.
func (p *api) statsBudget(ctx context.Context, req gossiprpc.Request) error {
	if err := p.debitMojangUnlessUUID(ctx, req.Account, req.IsPremium); err != nil {
		return err
	}
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

// accountKey normalizes the player identifier for cache keys.
func accountKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

// --- lifetime stats --------------------------------------------------------------

// playerResponse is the api.hypixel.net/v2/player envelope subset gossip
// reads. Player is null for an unknown uuid even on a 200.
type playerResponse struct {
	Success bool `json:"success"`
	Player  *struct {
		DisplayName  string `json:"displayname"`
		Achievements struct {
			BedwarsLevel int64 `json:"bedwars_level"`
		} `json:"achievements"`
		Stats struct {
			Bedwars struct {
				Wins        int64 `json:"wins_bedwars"`
				Losses      int64 `json:"losses_bedwars"`
				FinalKills  int64 `json:"final_kills_bedwars"`
				FinalDeaths int64 `json:"final_deaths_bedwars"`
				BedsBroken  int64 `json:"beds_broken_bedwars"`
			} `json:"Bedwars"`
		} `json:"stats"`
	} `json:"player"`
}

// fetchStats resolves the uuid, spends the Hypixel budget, and shapes the
// success reply.
func (p *api) fetchStats(ctx context.Context, account string) (gossiprpc.HypixelStatsReply, error) {
	uuid, err := p.resolveUUID(ctx, account)
	if err != nil {
		return gossiprpc.HypixelStatsReply{}, err
	}
	var resp playerResponse
	if err := p.http.GetJSON(ctx, "/v2/player", url.Values{"uuid": {uuid}}, &resp); err != nil {
		return gossiprpc.HypixelStatsReply{}, err
	}
	if resp.Player == nil {
		// Hypixel answers 200 with player:null for an unknown uuid; shape it
		// like a 404 so it negative-caches and chats "player not found".
		return gossiprpc.HypixelStatsReply{}, &core.UpstreamError{Status: 404, Message: "player not found"}
	}

	name := resp.Player.DisplayName
	if name == "" {
		name = account
	}
	bw := resp.Player.Stats.Bedwars
	return gossiprpc.HypixelStatsReply{
		Player:      name,
		Stars:       resp.Player.Achievements.BedwarsLevel,
		Wins:        bw.Wins,
		Losses:      bw.Losses,
		FinalKills:  bw.FinalKills,
		FinalDeaths: bw.FinalDeaths,
		BedsBroken:  bw.BedsBroken,
	}, nil
}
