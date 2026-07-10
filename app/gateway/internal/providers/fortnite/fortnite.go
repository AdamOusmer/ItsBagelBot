// Package fortnite is the gateway provider behind sesame's !fnstats and
// !store. It rides two external systems:
//
//   - Item shop: fortnite-api.com /v2/shop, a public endpoint (no key).
//   - Player stats: api-fortnite.com (prod.api-fortnite.com, x-api-key), the
//     replacement for fortnite-api.com's key-gated stats. The flow is two
//     calls: /api/v1/account/displayName/{name} resolves the Epic account id
//     (cached a day), then /api/v2/stats/{id} answers Epic's raw stats-v2
//     counter blob — one br_<metric>_<input>_m0_playlist_<playlist> key per
//     counter — which the gateway aggregates down to the bot-needed values:
//     wins, matches, kills, K/D, win rate, and the solo/duo/squad breakdown.
//
// The season window rides the stats endpoint's startTime filter, so it needs
// the current season's start epoch (FORTNITE_SEASON_START_UNIX); unset, a
// season request degrades to lifetime and says so in the reply's window.
// Platform lookups (PSN/Xbox) are Pro-plan features upstream and answer a
// friendly error for now. All endpoints are byte-flow: the reply is shaped
// and marshaled once on fetch, and a cache hit answers with the stored wire
// bytes untouched.
package fortnite

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
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
	// accountTTL: an Epic display-name -> account-id binding only changes on a
	// rename.
	accountTTL = 24 * time.Hour
	// shopTTL: the shop rotates once a day (00:00 UTC), so a 15-minute lag on
	// rotation day is invisible against a 24h cycle.
	shopTTL = 15 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	// shopWindowSeconds is the fortnite-api.com budget window; it publishes no
	// hard per-key budget, so the limit stays a conservative per-minute
	// allowance.
	shopWindowSeconds = 60.0
	// statsWindowSeconds is the api-fortnite.com budget window: the free plan
	// caps requests per day.
	statsWindowSeconds = 86400.0
)

// Config carries the provider's environment. The shop upstream is public;
// the stats upstream (api-fortnite.com) requires APIKey, so keyless the
// provider runs shop-only mode: the stats endpoint is not registered and
// !fnstats times out at the caller like any disabled provider.
type Config struct {
	// ShopBaseURL is the fortnite-api.com host serving /v2/shop.
	ShopBaseURL string
	// StatsBaseURL is the api-fortnite.com host serving account lookups and
	// raw stats.
	StatsBaseURL string
	// APIKey is the api-fortnite.com key, sent as x-api-key on stats calls.
	APIKey string
	// ShopRateLimit is shop requests per minute.
	ShopRateLimit float64
	// StatsRateLimit is stats-upstream requests per day (the free plan allows
	// 10k; the default leaves headroom).
	StatsRateLimit float64
	// SeasonStartUnix is the current season's start epoch, driving the
	// "season" stats window; 0 degrades season requests to lifetime.
	SeasonStartUnix int64
}

// Provider implements provider.Provider for the fortnite systems.
type Provider struct {
	shop  *core.HTTPClient
	stats *core.HTTPClient
	cache *core.Cache
	log   *zap.Logger

	deps        provider.Deps
	shopBucket  core.Buckets
	statsBucket core.Buckets
	seasonStart int64
	// keyed reports whether the stats key is configured; without it the stats
	// endpoint is not served (shop-only mode).
	keyed bool
}

// New builds the fortnite provider.
func New(cfg Config, d provider.Deps) *Provider {
	shopBase := strings.TrimSuffix(cfg.ShopBaseURL, "/")
	if shopBase == "" {
		shopBase = "https://fortnite-api.com"
	}
	statsBase := strings.TrimSuffix(cfg.StatsBaseURL, "/")
	if statsBase == "" {
		statsBase = "https://prod.api-fortnite.com"
	}
	if cfg.ShopRateLimit <= 0 {
		cfg.ShopRateLimit = 120
	}
	if cfg.StatsRateLimit <= 0 {
		cfg.StatsRateLimit = 9000
	}
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}
	var statsHeaders map[string]string
	if cfg.APIKey != "" {
		statsHeaders = map[string]string{"x-api-key": cfg.APIKey}
	}
	return &Provider{
		shop:        core.NewHTTPClient(shopBase, nil, httpTimeout),
		stats:       core.NewHTTPClient(statsBase, statsHeaders, httpTimeout),
		cache:       d.Cache,
		log:         log,
		deps:        d,
		shopBucket:  core.NewBuckets("ratelimit:gateway:fortnite", cfg.ShopRateLimit, shopWindowSeconds),
		statsBucket: core.NewBuckets("ratelimit:gateway:fortnite:stats", cfg.StatsRateLimit, statsWindowSeconds),
		seasonStart: cfg.SeasonStartUnix,
		keyed:       cfg.APIKey != "",
	}
}

func (p *Provider) Name() string { return "fortnite" }

func (p *Provider) Endpoints() []provider.Endpoint {
	eps := []provider.Endpoint{
		{Name: "shop", Timeout: handlerTimeout, Handle: p.shopEndpoint},
	}
	if p.keyed {
		eps = append(eps, provider.Endpoint{Name: "stats", Timeout: handlerTimeout, Handle: p.statsEndpoint})
	}
	return eps
}

// normalizeWindow maps the dashboard's window setting onto the window the
// provider can actually serve: "season" only when the season start epoch is
// configured, otherwise lifetime.
func (p *Provider) normalizeWindow(w string) string {
	if strings.ToLower(strings.TrimSpace(w)) == "season" && p.seasonStart > 0 {
		return "season"
	}
	return "lifetime"
}

// epicOnly answers the friendly error for platform lookups the upstream's
// free plan cannot do ("" when the type is fine). Blank defaults to epic.
func epicOnly(accountType string) string {
	switch strings.ToLower(strings.TrimSpace(accountType)) {
	case "", "epic":
		return ""
	default:
		return "only Epic display names are supported right now"
	}
}

// --- account resolution ------------------------------------------------------

// accountRef is the cached displayName lookup result: the Epic account id and
// the canonically-cased display name.
type accountRef struct {
	ID   string `json:"id"`
	Name string `json:"displayName"`
}

// friendly404 rewrites an upstream 404 (whose body is the wordy "Upstream API
// error: ..." passthrough) into the chat-sized message.
func friendly404(err error, msg string) error {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && ue.Status == 404 {
		return &core.UpstreamError{Status: 404, Message: msg}
	}
	return err
}

// resolveAccount turns a display name into the account ref via the stats
// upstream, cached for a day. An unknown name 404s and negative-caches.
func (p *Provider) resolveAccount(ctx context.Context, account string, isPremium bool) (accountRef, error) {
	key := core.Key(p.Name(), "account", strings.ToLower(account))
	return core.Cached(ctx, p.cache, key, accountTTL, negativeTTL, func(ctx context.Context) (accountRef, error) {
		if err := p.statsBucket.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
			return accountRef{}, err
		}
		var ref accountRef
		if err := p.stats.GetJSON(ctx, "/api/v1/account/displayName/"+url.PathEscape(account), nil, &ref); err != nil {
			return accountRef{}, friendly404(err, "player not found")
		}
		if ref.ID == "" {
			return accountRef{}, &core.UpstreamError{Status: 404, Message: "player not found"}
		}
		return ref, nil
	})
}

// --- stats ---------------------------------------------------------------------

// rawStatsResponse is the /api/v2/stats/{accountId} body: Epic's raw stats-v2
// counters keyed br_<metric>_<input>_m0_playlist_<playlist>, plus unrelated
// counters (battle-pass levels, arena leftovers) the key pattern filters out.
type rawStatsResponse struct {
	AccountID string             `json:"accountId"`
	Stats     map[string]float64 `json:"stats"`
}

// statKeyRe picks the three counters the bot reads out of a raw stats key and
// captures (metric, playlist). The input segment (keyboardmouse/gamepad/touch)
// is summed over.
var statKeyRe = regexp.MustCompile(`^br_(placetop1|kills|matchesplayed)_(?:keyboardmouse|gamepad|touch)_m0_playlist_(.+)$`)

// coreModes maps the core Battle Royale playlists — build and zero-build pubs
// — onto the reply's mode breakdown. Ranked (habanero) and LTM playlists count
// only toward the overall roll-up.
var coreModes = map[string]int{
	"defaultsolo": 0, "nobuildbr_solo": 0,
	"defaultduo": 1, "nobuildbr_duo": 1,
	"defaultsquad": 2, "nobuildbr_squad": 2,
}

// modeAgg accumulates one bucket's counters before the derived values are
// computed.
type modeAgg struct {
	wins, matches, kills int64
}

// reply computes the derived K/D and win rate. Deaths are matches minus wins
// (Epic tracks no death counter for BR); a flawless record divides by one.
func (a modeAgg) reply() gatewayrpc.FortniteModeStats {
	deaths := a.matches - a.wins
	if deaths <= 0 {
		deaths = 1
	}
	winRate := 0.0
	if a.matches > 0 {
		winRate = float64(a.wins) * 100 / float64(a.matches)
	}
	return gatewayrpc.FortniteModeStats{
		Wins:    a.wins,
		Matches: a.matches,
		Kills:   a.kills,
		KD:      float64(a.kills) / float64(deaths),
		WinRate: winRate,
	}
}

// add routes one counter into the aggregate.
func (a *modeAgg) add(metric string, v int64) {
	switch metric {
	case "placetop1":
		a.wins += v
	case "kills":
		a.kills += v
	case "matchesplayed":
		a.matches += v
	}
}

// aggregate folds the raw counter blob into the overall and per-mode buckets:
// overall spans every playlist (LTMs and ranked included), the mode buckets
// span the core pubs playlists only.
func aggregate(stats map[string]float64) (overall modeAgg, modes [3]modeAgg) {
	for key, val := range stats {
		m := statKeyRe.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		metric, playlist := m[1], m[2]
		overall.add(metric, int64(val))
		if idx, ok := coreModes[playlist]; ok {
			modes[idx].add(metric, int64(val))
		}
	}
	return overall, modes
}

// statsQuery is one normalized stats lookup.
type statsQuery struct {
	account string
	window  string
}

// statsEndpoint answers fortnite.stats (sesame's !fnstats) with the player's
// aggregated Battle Royale stats over the requested window.
func (p *Provider) statsEndpoint(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.FortniteStatsReply{Error: "missing account"}
	}
	if msg := epicOnly(req.AccountType); msg != "" {
		return gatewayrpc.FortniteStatsReply{Player: account, Error: msg}
	}
	q := statsQuery{account: account, window: p.normalizeWindow(req.TimeWindow)}

	key := core.Key(p.Name(), "stats", q.window+":"+strings.ToLower(account))
	b, err := core.CachedBytes(ctx, p.cache, key, func(ctx context.Context) ([]byte, time.Duration, error) {
		return core.BuildReply(ctx, statsTTL, negativeTTL,
			func(ctx context.Context) (any, error) { return p.fetchStats(ctx, q, req.IsPremium) },
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

// fetchStats resolves the account, spends the budget, pulls the raw counter
// blob (window-filtered for season) and shapes the success reply.
func (p *Provider) fetchStats(ctx context.Context, q statsQuery, isPremium bool) (gatewayrpc.FortniteStatsReply, error) {
	ref, err := p.resolveAccount(ctx, q.account, isPremium)
	if err != nil {
		return gatewayrpc.FortniteStatsReply{}, err
	}
	if err := p.statsBucket.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
		return gatewayrpc.FortniteStatsReply{}, err
	}

	var query url.Values
	if q.window == "season" {
		query = url.Values{"startTime": {strconv.FormatInt(p.seasonStart, 10)}}
	}
	var resp rawStatsResponse
	if err := p.stats.GetJSON(ctx, "/api/v2/stats/"+url.PathEscape(ref.ID), query, &resp); err != nil {
		return gatewayrpc.FortniteStatsReply{}, friendly404(err, "no stats for this player")
	}

	overall, modes := aggregate(resp.Stats)
	return gatewayrpc.FortniteStatsReply{
		Player:  ref.Name,
		Window:  q.window,
		Overall: overall.reply(),
		Solo:    modes[0].reply(),
		Duo:     modes[1].reply(),
		Squad:   modes[2].reply(),
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

// shopEndpoint answers fortnite.shop (sesame's !store) with the current
// item-shop rotation. The shop is global, so the cache key carries no request
// state.
func (p *Provider) shopEndpoint(ctx context.Context, req gatewayrpc.Request) any {
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
	if err := p.shopBucket.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
		return gatewayrpc.FortniteShopReply{}, err
	}

	var resp shopResponse
	if err := p.shop.GetJSON(ctx, "/v2/shop", nil, &resp); err != nil {
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
