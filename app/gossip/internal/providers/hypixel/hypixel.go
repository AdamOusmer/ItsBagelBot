// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	statsTTL    = 10 * time.Minute
	negativeTTL = 5 * time.Minute
	uuidTTL     = 24 * time.Hour

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	rateWindowSeconds   = 300.0
	mojangWindowSeconds = 600.0
)

type Config struct {
	BaseURL         string
	MojangBaseURL   string
	APIKey          string
	RateLimit       float64
	MojangRateLimit float64
}

const providerName = "hypixel"

type api struct {
	http          *core.HTTPClient
	mojang        *core.HTTPClient
	cache         *core.Cache
	log           *zap.Logger
	limiter       *ratelimit.Limiter
	buckets       core.Buckets
	mojangBuckets core.Buckets
}

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

func statsErrReply(id, msg string) any {
	return gossiprpc.HypixelStatsReply{Player: id, Error: msg}
}

func (p *api) statsFetch(ctx context.Context, _ gossiprpc.Request, id provider.ID) (any, error) {
	return p.fetchStats(ctx, id.Display)
}

func (p *api) statsBudget(ctx context.Context, req gossiprpc.Request) error {
	if err := p.debitMojangUnlessUUID(ctx, req.Account, req.IsPremium); err != nil {
		return err
	}
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

func accountKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

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
