// Package fortnite is the gateway provider for fortnite-api.com: Battle
// Royale player stats for sesame's !fnstats and the daily item shop for
// !store.
//
// The stats endpoint looks a player up by display name inside a platform
// namespace (Epic, PlayStation or Xbox) over either the lifetime or the
// current-season window — both are dashboard settings the request carries.
// The reply is normalized to the bot-needed values only: wins, matches,
// kills, K/D, win rate, and the solo/duo/squad mode breakdown. All endpoints
// are byte-flow: the reply is shaped and marshaled once on fetch, and a cache
// hit answers with the stored wire bytes untouched.
package fortnite

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"go.uber.org/zap"
)

const (
	// statsTTL matches the other stats providers' staleness budget.
	statsTTL    = 10 * time.Minute
	negativeTTL = 5 * time.Minute
	// shopTTL: the shop rotates once a day (00:00 UTC), so a 15-minute lag on
	// rotation day is invisible against a 24h cycle.
	shopTTL = 15 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	// rateWindowSeconds is the budget window the configured limit spans.
	// fortnite-api.com does not publish a hard per-key budget, so the limit
	// stays a conservative per-minute allowance.
	rateWindowSeconds = 60.0
)

// Config carries the provider's environment. The stats endpoint requires the
// API key (sent as the Authorization header); main skips the provider without
// one. RateLimit is requests per minute.
type Config struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
}

// Provider implements provider.Provider for the fortnite-api.com API.
type Provider struct {
	http  *core.HTTPClient
	cache *core.Cache
	log   *zap.Logger

	deps    provider.Deps
	buckets core.Buckets
}

// New builds the fortnite provider.
func New(cfg Config, d provider.Deps) *Provider {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://fortnite-api.com"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 120
	}
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Provider{
		http:    core.NewHTTPClient(base, map[string]string{"Authorization": cfg.APIKey}, httpTimeout),
		cache:   d.Cache,
		log:     log,
		deps:    d,
		buckets: core.NewBuckets("ratelimit:gateway:fortnite", cfg.RateLimit, rateWindowSeconds),
	}
}

func (p *Provider) Name() string { return "fortnite" }

func (p *Provider) Endpoints() []provider.Endpoint {
	return []provider.Endpoint{
		{Name: "stats", Timeout: handlerTimeout, Handle: p.stats},
		{Name: "shop", Timeout: handlerTimeout, Handle: p.shop},
	}
}

// normalizeAccountType maps the dashboard's account-type setting onto the
// upstream's accountType parameter; anything unrecognized (including blank)
// falls back to Epic, the platform every Fortnite account ultimately lives on.
func normalizeAccountType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "psn":
		return "psn"
	case "xbl":
		return "xbl"
	default:
		return "epic"
	}
}

// normalizeTimeWindow maps the dashboard's window setting onto the upstream's
// timeWindow parameter; anything but "season" means lifetime.
func normalizeTimeWindow(w string) string {
	if strings.ToLower(strings.TrimSpace(w)) == "season" {
		return "season"
	}
	return "lifetime"
}

// --- stats -----------------------------------------------------------------------

// modeStats is the upstream per-queue stat block subset the gateway reads.
// Queues the player never touched are null in the JSON, so the reply reads
// them through pointers.
type modeStats struct {
	Wins    int64   `json:"wins"`
	Matches int64   `json:"matches"`
	Kills   int64   `json:"kills"`
	KD      float64 `json:"kd"`
	WinRate float64 `json:"winRate"`
}

// statsResponse is the /v2/stats/br/v2 envelope subset the gateway reads.
// stats.all aggregates every input type; the per-input blocks are ignored.
type statsResponse struct {
	Data struct {
		Account struct {
			Name string `json:"name"`
		} `json:"account"`
		Stats struct {
			All struct {
				Overall *modeStats `json:"overall"`
				Solo    *modeStats `json:"solo"`
				Duo     *modeStats `json:"duo"`
				Squad   *modeStats `json:"squad"`
			} `json:"all"`
		} `json:"stats"`
	} `json:"data"`
}

// toReplyStats flattens a possibly-null upstream queue block.
func toReplyStats(m *modeStats) gatewayrpc.FortniteModeStats {
	if m == nil {
		return gatewayrpc.FortniteModeStats{}
	}
	return gatewayrpc.FortniteModeStats{
		Wins: m.Wins, Matches: m.Matches, Kills: m.Kills, KD: m.KD, WinRate: m.WinRate,
	}
}

// privateStatsError reshapes the upstream 403 for an account whose "Public
// Game Stats" toggle is off into a player-level 404-class failure: it chats a
// specific message and negative-caches (private stays private for a while).
// A 403 that does not mention the stats toggle (bad or unauthorized API key)
// passes through and maps to the generic non-cached config-problem message.
func privateStatsError(err error) error {
	var ue *core.UpstreamError
	if !errors.As(err, &ue) || ue.Status != 403 {
		return err
	}
	msg := strings.ToLower(ue.Message)
	if strings.Contains(msg, "public") || strings.Contains(msg, "private") || strings.Contains(msg, "stats") {
		return &core.UpstreamError{Status: 404, Message: "this player's game stats are private"}
	}
	return err
}

// stats answers fortnite.stats (sesame's !fnstats) with the player's
// normalized Battle Royale stats over the requested window.
func (p *Provider) stats(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.FortniteStatsReply{Error: "missing account"}
	}
	accountType := normalizeAccountType(req.AccountType)
	window := normalizeTimeWindow(req.TimeWindow)

	key := core.Key(p.Name(), "stats", accountType+":"+window+":"+strings.ToLower(account))
	b, err := core.CachedBytes(ctx, p.cache, key, func(ctx context.Context) ([]byte, time.Duration, error) {
		return core.BuildReply(ctx, statsTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				return p.fetchStats(ctx, account, accountType, window, req.IsPremium)
			},
			func(msg string) any { return gatewayrpc.FortniteStatsReply{Player: account, Error: msg} },
		)
	})
	if err != nil {
		p.log.Warn("fortnite stats fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.FortniteStatsReply{Player: account, Error: "stats lookup failed"}
	}
	// Pre-marshaled wire bytes: the engine responds with these untouched.
	return json.RawMessage(b)
}

// fetchStats spends the budget, queries /v2/stats/br/v2 and shapes the
// success reply.
func (p *Provider) fetchStats(ctx context.Context, account, accountType, window string, isPremium bool) (gatewayrpc.FortniteStatsReply, error) {
	if err := p.buckets.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
		return gatewayrpc.FortniteStatsReply{}, err
	}

	query := url.Values{
		"name":        {account},
		"accountType": {accountType},
		"timeWindow":  {window},
		"image":       {"none"},
	}
	var resp statsResponse
	if err := p.http.GetJSON(ctx, "/v2/stats/br/v2", query, &resp); err != nil {
		return gatewayrpc.FortniteStatsReply{}, privateStatsError(err)
	}

	name := resp.Data.Account.Name
	if name == "" {
		name = account
	}
	all := resp.Data.Stats.All
	return gatewayrpc.FortniteStatsReply{
		Player:  name,
		Window:  window,
		Overall: toReplyStats(all.Overall),
		Solo:    toReplyStats(all.Solo),
		Duo:     toReplyStats(all.Duo),
		Squad:   toReplyStats(all.Squad),
	}, nil
}

// --- item shop -------------------------------------------------------------------

// named is the {"name": ...} shape shop items of every kind share.
type named struct {
	Name string `json:"name"`
}

// titled is the {"title": ...} shape jam tracks use instead of a name.
type titled struct {
	Title string `json:"title"`
}

// shopEntry is one /v2/shop offer subset: the final price plus enough of each
// item family to pick a display name.
type shopEntry struct {
	FinalPrice  int64    `json:"finalPrice"`
	Bundle      *named   `json:"bundle"`
	BrItems     []named  `json:"brItems"`
	Instruments []named  `json:"instruments"`
	Cars        []named  `json:"cars"`
	LegoKits    []named  `json:"legoKits"`
	Tracks      []titled `json:"tracks"`
}

// shopResponse is the /v2/shop envelope subset the gateway reads: the rotation
// date plus the offers.
type shopResponse struct {
	Data struct {
		Date    string      `json:"date"`
		Entries []shopEntry `json:"entries"`
	} `json:"data"`
}

// shop answers fortnite.shop (sesame's !store) with the current item-shop
// rotation. The shop is global, so the cache key carries no request state.
func (p *Provider) shop(ctx context.Context, req gatewayrpc.Request) any {
	key := core.Key(p.Name(), "shop", "current")
	b, err := core.CachedBytes(ctx, p.cache, key, func(ctx context.Context) ([]byte, time.Duration, error) {
		return core.BuildReply(ctx, shopTTL, negativeTTL,
			func(ctx context.Context) (any, error) { return p.fetchShop(ctx, req.IsPremium) },
			func(msg string) any { return gatewayrpc.FortniteShopReply{Error: msg} },
		)
	})
	if err != nil {
		p.log.Warn("fortnite shop fetch failed", zap.Error(err))
		return gatewayrpc.FortniteShopReply{Error: "item shop lookup failed"}
	}
	return json.RawMessage(b)
}

// fetchShop spends the budget, queries /v2/shop and normalizes each offer to
// name + final price. Offers with nothing displayable are dropped.
func (p *Provider) fetchShop(ctx context.Context, isPremium bool) (gatewayrpc.FortniteShopReply, error) {
	if err := p.buckets.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
		return gatewayrpc.FortniteShopReply{}, err
	}

	var resp shopResponse
	if err := p.http.GetJSON(ctx, "/v2/shop", nil, &resp); err != nil {
		return gatewayrpc.FortniteShopReply{}, err
	}

	entries := make([]gatewayrpc.FortniteShopEntry, 0, len(resp.Data.Entries))
	for _, e := range resp.Data.Entries {
		name := e.displayName()
		if name == "" {
			continue
		}
		entries = append(entries, gatewayrpc.FortniteShopEntry{Name: name, Price: e.FinalPrice})
	}
	// The upstream date is an ISO timestamp; the reply carries the day only.
	date, _, _ := strings.Cut(resp.Data.Date, "T")
	return gatewayrpc.FortniteShopReply{Date: date, Count: len(entries), Entries: entries}, nil
}

// displayName picks the offer's chat name: the bundle's own name when the
// offer is a bundle, otherwise the lead item of the first non-empty family.
func (e shopEntry) displayName() string {
	if e.Bundle != nil && e.Bundle.Name != "" {
		return e.Bundle.Name
	}
	for _, family := range [][]named{e.BrItems, e.Instruments, e.Cars, e.LegoKits} {
		if len(family) > 0 && family[0].Name != "" {
			return family[0].Name
		}
	}
	if len(e.Tracks) > 0 {
		return e.Tracks[0].Title
	}
	return ""
}
