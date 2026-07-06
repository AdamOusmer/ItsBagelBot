// Package hypixel is the gateway provider for the direct Hypixel API: lifetime
// Bed Wars stats for sesame's !bwstats. It is its own provider — not a path
// inside urchin — because it is a separate external system with its own key,
// its own (much smaller) rate budget and its own failure modes; on the
// dashboard the command still lives on the one urchin module page, which is a
// sesame/console concern, not a gateway one.
//
// The player identifier resolves through Mojang's public profile endpoint, so
// this provider depends on no other provider. All endpoints are byte-flow: the
// reply is shaped and marshaled once on fetch, and a cache hit answers with
// the stored wire bytes untouched.
package hypixel

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

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
)

// Config carries the provider's environment. APIKey empty = provider disabled
// (main skips it). RateLimit is requests per 5 minutes; Hypixel personal keys
// allow 300.
type Config struct {
	BaseURL       string
	MojangBaseURL string
	APIKey        string
	RateLimit     float64
}

// Provider implements provider.Provider for the Hypixel API.
type Provider struct {
	http   *core.HTTPClient
	mojang *core.HTTPClient
	cache  *core.Cache
	log    *zap.Logger

	deps    provider.Deps
	buckets core.Buckets
}

// New builds the hypixel provider.
func New(cfg Config, d provider.Deps) *Provider {
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
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Provider{
		http:    core.NewHTTPClient(base, map[string]string{"API-Key": cfg.APIKey}, httpTimeout),
		mojang:  core.NewHTTPClient(mojangBase, nil, httpTimeout),
		cache:   d.Cache,
		log:     log,
		deps:    d,
		buckets: core.NewBuckets("ratelimit:gateway:hypixel", cfg.RateLimit, rateWindowSeconds),
	}
}

func (p *Provider) Name() string { return "hypixel" }

func (p *Provider) Endpoints() []provider.Endpoint {
	return []provider.Endpoint{
		{Name: "stats", Timeout: handlerTimeout, Handle: p.stats},
	}
}

// accountKey normalizes the player identifier for cache keys.
func accountKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

// --- uuid resolution (Mojang) --------------------------------------------------

// mojangProfile is the api.mojang.com profile lookup body.
type mojangProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// looksLikeUUID reports whether account is already a uuid (32 hex chars,
// dashes optional), in which case Mojang is skipped.
func looksLikeUUID(account string) bool {
	n := 0
	for _, r := range account {
		switch {
		case r == '-':
			continue
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
			n++
		default:
			return false
		}
	}
	return n == 32
}

// resolveUUID turns a username into the canonical uuid via Mojang, cached for
// a day. An unknown name is a 404 there already (204 on the legacy path is
// also treated as missing by the empty-id check), so it negative-caches.
func (p *Provider) resolveUUID(ctx context.Context, account string) (string, error) {
	if looksLikeUUID(account) {
		return strings.ReplaceAll(account, "-", ""), nil
	}
	key := core.Key(p.Name(), "uuid", accountKey(account))
	return core.Cached(ctx, p.cache, key, uuidTTL, negativeTTL, func(ctx context.Context) (string, error) {
		var profile mojangProfile
		if err := p.mojang.GetJSON(ctx, "/users/profiles/minecraft/"+account, nil, &profile); err != nil {
			return "", err
		}
		if strings.TrimSpace(profile.ID) == "" {
			return "", &core.UpstreamError{Status: 404, Message: "player not found"}
		}
		return profile.ID, nil
	})
}

// --- lifetime stats --------------------------------------------------------------

// playerResponse is the api.hypixel.net/v2/player envelope subset the gateway
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

// stats answers hypixel.stats (sesame's !bwstats) with the player's lifetime
// Bed Wars stats.
func (p *Provider) stats(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.HypixelStatsReply{Error: "missing account"}
	}
	key := core.Key(p.Name(), "stats", accountKey(account))
	b, err := core.CachedBytes(ctx, p.cache, key, func(ctx context.Context) ([]byte, time.Duration, error) {
		return core.BuildReply(ctx, statsTTL, negativeTTL,
			func(ctx context.Context) (any, error) { return p.fetchStats(ctx, account, req.IsPremium) },
			func(msg string) any { return gatewayrpc.HypixelStatsReply{Player: account, Error: msg} },
		)
	})
	if err != nil {
		p.log.Warn("hypixel stats fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.HypixelStatsReply{Player: account, Error: "stats lookup failed"}
	}
	// Pre-marshaled wire bytes: the engine responds with these untouched.
	return json.RawMessage(b)
}

// fetchStats resolves the uuid, spends the Hypixel budget, and shapes the
// success reply.
func (p *Provider) fetchStats(ctx context.Context, account string, isPremium bool) (gatewayrpc.HypixelStatsReply, error) {
	uuid, err := p.resolveUUID(ctx, account)
	if err != nil {
		return gatewayrpc.HypixelStatsReply{}, err
	}
	if err := p.buckets.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
		return gatewayrpc.HypixelStatsReply{}, err
	}

	var resp playerResponse
	if err := p.http.GetJSON(ctx, "/v2/player", url.Values{"uuid": {uuid}}, &resp); err != nil {
		return gatewayrpc.HypixelStatsReply{}, err
	}
	if resp.Player == nil {
		// Hypixel answers 200 with player:null for an unknown uuid; shape it
		// like a 404 so it negative-caches and chats "player not found".
		return gatewayrpc.HypixelStatsReply{}, &core.UpstreamError{Status: 404, Message: "player not found"}
	}

	name := resp.Player.DisplayName
	if name == "" {
		name = account
	}
	bw := resp.Player.Stats.Bedwars
	return gatewayrpc.HypixelStatsReply{
		Player:      name,
		Stars:       resp.Player.Achievements.BedwarsLevel,
		Wins:        bw.Wins,
		Losses:      bw.Losses,
		FinalKills:  bw.FinalKills,
		FinalDeaths: bw.FinalDeaths,
		BedsBroken:  bw.BedsBroken,
	}, nil
}
