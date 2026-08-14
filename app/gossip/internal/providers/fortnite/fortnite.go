// Package fortnite is the gossip provider behind sesame's !fnstats and
// !store. It rides two external systems:
//
//   - Item shop: fortnite-api.com /v2/shop, a public endpoint (no key).
//   - Player stats: api-fortnite.com (prod.api-fortnite.com, x-api-key), the
//     replacement for fortnite-api.com's key-gated stats. The flow is two
//     calls: /api/v1/account/displayName/{name} resolves the Epic account id
//     (cached a day), then /api/v2/stats/{id} answers Epic's raw stats-v2
//     counter blob — one br_<metric>_<input>_m0_playlist_<playlist> key per
//     counter — which gossip aggregates down to the bot-needed values:
//     wins, matches, kills, K/D, win rate, and the solo/duo/squad breakdown.
//
// The season window rides the stats endpoint's startTime filter. The current
// season's start epoch comes from the upstream's own /api/v1/season (cached
// an hour, so a season rollover is picked up automatically);
// FORTNITE_SEASON_START_UNIX overrides it manually, and if neither yields a
// start the season request degrades to lifetime and says so in the reply's
// window. Platform lookups (PSN/Xbox) are Pro-plan features upstream and
// answer a friendly error for now. All endpoints are byte-flow: the reply is shaped
// and marshaled once on fetch, and a cache hit answers with the stored wire
// bytes untouched.
package fortnite

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	// statsTTL matches the other stats providers' staleness budget.
	statsTTL    = 10 * time.Minute
	negativeTTL = 5 * time.Minute
	// accountTTL: an Epic display-name -> account-id binding only changes on a
	// rename.
	accountTTL = 24 * time.Hour
	// seasonTTL bounds how stale the auto-fetched season start may run; a
	// season rollover (4x a year) is picked up within the hour.
	seasonTTL = time.Hour
	// shopTTL: the shop rotates once a day (00:00 UTC), so a 15-minute lag on
	// rotation day is invisible against a 24h cycle.
	shopTTL = 15 * time.Minute
	// sessionSnapshotTTL outlives any plausible single stream; Twitch caps
	// broadcasts at 48h.
	sessionSnapshotTTL = 49 * time.Hour

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
	// SeasonStartUnix manually overrides the season window's start epoch. 0
	// (the default) auto-resolves it from the upstream's /api/v1/season.
	SeasonStartUnix int64
}

// providerName is the subject token this provider answers under.
const providerName = "fortnite"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	shop  *core.HTTPClient
	stats *core.HTTPClient
	cache *core.Cache
	log   *zap.Logger

	deps        provider.Deps
	shopBucket  core.Buckets
	statsBucket core.Buckets
	seasonStart int64
	// keyed reports whether the stats key is configured; without it the stats
	// endpoints are not served (shop-only mode).
	keyed bool
}

// New builds the fortnite provider. The shop endpoint always serves; the
// keyed stats/session endpoints register only when the api-fortnite.com key is
// configured, so shop-only mode simply never subscribes them.
func New(cfg Config, d provider.Deps) provider.Provider {
	return newAPI(cfg, d).build()
}

func (p *api) build() provider.Provider {
	b := provider.NewProvider(providerName, p.deps)
	b.Endpoint("shop").Timeout(handlerTimeout).
		Cached(shopTTL, negativeTTL).
		ID(provider.StaticID("current")).
		Reply(shopErrReply).
		Fallback("item shop lookup failed").
		Fetch(p.shopFetch)
	if p.keyed {
		b.Endpoint("stats").Timeout(handlerTimeout).
			Cached(statsTTL, negativeTTL).
			ID(statsID).
			Reply(statsErrReply).
			Fallback("stats lookup failed").
			Fetch(p.statsFetch)
		b.Endpoint("session_start").Timeout(handlerTimeout).Handle(p.sessionStart)
		b.Endpoint("session").Timeout(handlerTimeout).Handle(p.session)
	}
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
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
	var statsHeaders map[string]string
	if cfg.APIKey != "" {
		statsHeaders = map[string]string{"x-api-key": cfg.APIKey}
	}
	return &api{
		shop:        core.NewHTTPClient(shopBase, nil, httpTimeout),
		stats:       core.NewHTTPClient(statsBase, statsHeaders, httpTimeout),
		cache:       d.Cache,
		log:         d.Logger(),
		deps:        d,
		shopBucket:  core.NewBuckets("ratelimit:gossip:fortnite", cfg.ShopRateLimit, shopWindowSeconds),
		statsBucket: core.NewBuckets("ratelimit:gossip:fortnite:stats", cfg.StatsRateLimit, statsWindowSeconds),
		seasonStart: cfg.SeasonStartUnix,
		keyed:       cfg.APIKey != "",
	}
}

func shopErrReply(_, msg string) any { return gossiprpc.FortniteShopReply{Error: msg} }
func statsErrReply(id, msg string) any {
	return gossiprpc.FortniteStatsReply{Player: id, Error: msg}
}

// normalizeWindow maps the dashboard's window setting onto the requested
// window; whether "season" can actually be served is decided at fetch time
// (seasonStartTime), where it may degrade to lifetime.
func normalizeWindow(w string) string {
	if strings.ToLower(strings.TrimSpace(w)) == "season" {
		return "season"
	}
	return "lifetime"
}

// seasonResponse is the /api/v1/season body subset gossip reads.
type seasonResponse struct {
	SeasonDateBegin time.Time `json:"seasonDateBegin"`
}

// seasonStartTime resolves the season window's start epoch: the manual
// override when configured, otherwise the upstream's own current-season
// begin date, cached an hour so a rollover is picked up automatically. 0
// means no start could be resolved (the caller degrades to lifetime).
func (p *api) seasonStartTime(ctx context.Context, isPremium bool) int64 {
	if p.seasonStart > 0 {
		return p.seasonStart
	}
	key := core.Key(providerName, "season", "start")
	start, err := core.Cached(ctx, p.cache, key, seasonTTL, negativeTTL, func(ctx context.Context) (int64, error) {
		if err := p.statsBucket.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
			return 0, err
		}
		var resp seasonResponse
		if err := p.stats.GetJSON(ctx, "/api/v1/season", nil, &resp); err != nil {
			return 0, err
		}
		return resp.SeasonDateBegin.Unix(), nil
	})
	if err != nil || start <= 0 {
		p.log.Warn("fortnite season start resolve failed, serving lifetime", zap.Error(err))
		return 0
	}
	return start
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
func (p *api) resolveAccount(ctx context.Context, account string, isPremium bool) (accountRef, error) {
	key := core.Key(providerName, "account", strings.ToLower(account))
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

// modeAgg accumulates one bucket's counters before the derived values are
// computed.
type modeAgg struct {
	wins, matches, kills int64
}

// reply computes the derived K/D and win rate. Deaths are matches minus wins
// (Epic tracks no death counter for BR); a flawless record divides by one.
func (a modeAgg) reply() gossiprpc.FortniteModeStats {
	deaths := a.matches - a.wins
	if deaths <= 0 {
		deaths = 1
	}
	winRate := 0.0
	if a.matches > 0 {
		winRate = float64(a.wins) * 100 / float64(a.matches)
	}
	return gossiprpc.FortniteModeStats{
		Wins:    a.wins,
		Matches: a.matches,
		Kills:   a.kills,
		KD:      float64(a.kills) / float64(deaths),
		WinRate: winRate,
	}
}

// statsQuery is one normalized stats lookup.
type statsQuery struct {
	account string
	window  string
}

// statsID validates the stats identity: an Epic display name, cache-keyed by
// the requested window as well as the account so season and lifetime replies
// never collide.
func statsID(req gossiprpc.Request) (provider.ID, string) {
	a := strings.TrimSpace(req.Account)
	if a == "" {
		return provider.ID{}, "missing account"
	}
	if msg := epicOnly(req.AccountType); msg != "" {
		return provider.ID{Display: a}, msg
	}
	return provider.ID{Display: a, Key: normalizeWindow(req.TimeWindow) + ":" + strings.ToLower(a)}, ""
}

// statsFetch answers fortnite.stats (sesame's !fnstats) with the player's
// aggregated Battle Royale stats over the requested window.
func (p *api) statsFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	q := statsQuery{account: id.Display, window: normalizeWindow(req.TimeWindow)}
	return p.fetchStats(ctx, q, req.IsPremium)
}

// resolveStatsWindow resolves the account and season window epoch. A cold
// season query fires both concurrently to overlap their network I/O, but the
// season leg runs under context.WithoutCancel(gctx) rather than gctx itself.
// seasonStartTime is shared infrastructure, not a per-request fetch: it rides
// core.Cached behind a singleflight keyed on the season cache key (see
// cache.go), so on a cold cache every in-flight request joins the same one
// upstream call. If that call inherited the account leg's cancellation, one
// caller's mistyped display name would abort it — and, because
// seasonStartTime swallows the error down to 0, silently downgrade every
// other joined caller's season request to lifetime for no reason of their
// own. Insulating the season leg costs at most one extra upstream call an
// hour (the cold-cache case cancellation was trying to save); it buys
// correctness for everyone else sharing the flight. The account error is
// still the only one that ever propagates from this function; season stays
// best-effort and degrades to 0 on its own failures regardless. The
// manual-override window (p.seasonStart set) has no I/O to overlap, so it
// skips the concurrent machinery entirely.
func (p *api) resolveStatsWindow(ctx context.Context, q statsQuery, isPremium bool) (accountRef, int64, error) {
	if q.window != "season" {
		ref, err := p.resolveAccount(ctx, q.account, isPremium)
		return ref, 0, err
	}
	if p.seasonStart > 0 {
		ref, err := p.resolveAccount(ctx, q.account, isPremium)
		return ref, p.seasonStart, err
	}

	var ref accountRef
	var season int64
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		r, err := p.resolveAccount(gctx, q.account, isPremium)
		ref = r
		return err
	})
	g.Go(func() error {
		season = p.seasonStartTime(context.WithoutCancel(gctx), isPremium)
		return nil
	})
	if err := g.Wait(); err != nil {
		return accountRef{}, 0, err
	}
	return ref, season, nil
}

// fetchStats resolves the account, spends the budget, pulls the raw counter
// blob (window-filtered for season) and shapes the success reply.
func (p *api) fetchStats(ctx context.Context, q statsQuery, isPremium bool) (gossiprpc.FortniteStatsReply, error) {
	ref, seasonStart, err := p.resolveStatsWindow(ctx, q, isPremium)
	if err != nil {
		return gossiprpc.FortniteStatsReply{}, err
	}
	if err := p.statsBucket.Enforce(ctx, p.deps.Limiter, isPremium); err != nil {
		return gossiprpc.FortniteStatsReply{}, err
	}

	var query url.Values
	if q.window == "season" {
		if seasonStart > 0 {
			query = url.Values{"startTime": {strconv.FormatInt(seasonStart, 10)}}
		} else {
			// No season start resolvable: serve lifetime and say so.
			q.window = "lifetime"
		}
	}
	var resp rawStatsResponse
	if err := p.stats.GetJSON(ctx, "/api/v2/stats/"+url.PathEscape(ref.ID), query, &resp); err != nil {
		return gossiprpc.FortniteStatsReply{}, friendly404(err, "no stats for this player")
	}

	return gossiprpc.FortniteStatsReply{
		Player:  ref.Name,
		Window:  q.window,
		Overall: resp.Overall.reply(),
		Solo:    resp.Modes[0].reply(),
		Duo:     resp.Modes[1].reply(),
		Squad:   resp.Modes[2].reply(),
	}, nil
}

// --- session -------------------------------------------------------------------

// snapshot is the stream-start standing stored per channel: the lifetime
// overall counters the session delta is diffed against. Session always tracks
// lifetime, never the season window — a season rollover mid-stream would
// corrupt a delta taken across it.
type snapshot struct {
	Account string `json:"account"`
	Player  string `json:"player"`
	Wins    int64  `json:"wins"`
	Matches int64  `json:"matches"`
	Kills   int64  `json:"kills"`
	AtUnix  int64  `json:"at_unix"`
}

func snapshotKey(channelID string) string { return core.Key("fortnite", "session", channelID) }

// cachedLifetimeStats reads a player's live lifetime stats behind the shared
// staleness budget. It is keyed apart from the byte-flow !fnstats path (that
// path stores pre-marshaled wire bytes, not the envelope core.Cached needs),
// so repeated !fn session calls within the window cost one upstream hit.
func (p *api) cachedLifetimeStats(ctx context.Context, account string, isPremium bool) (gossiprpc.FortniteStatsReply, error) {
	key := core.Key(providerName, "session-live", strings.ToLower(strings.TrimSpace(account)))
	return core.Cached(ctx, p.cache, key, statsTTL, negativeTTL, func(ctx context.Context) (gossiprpc.FortniteStatsReply, error) {
		return p.fetchStats(ctx, statsQuery{account: account, window: "lifetime"}, isPremium)
	})
}

// writeSnapshot stores the channel's stream-start standing under the snapshot
// key for sessionSnapshotTTL.
func (p *api) writeSnapshot(ctx context.Context, channelID, account string, stats gossiprpc.FortniteStatsReply) error {
	return p.cache.SetJSON(ctx, snapshotKey(channelID), snapshot{
		Account: strings.ToLower(account),
		Player:  stats.Player,
		Wins:    stats.Overall.Wins,
		Matches: stats.Overall.Matches,
		Kills:   stats.Overall.Kills,
		AtUnix:  time.Now().Unix(),
	}, sessionSnapshotTTL)
}

// sessionError maps an upstream failure to a friendly reply message, logging
// (as op) the infrastructure failures the friendly mapper does not name.
func (p *api) sessionError(op, account string, err error) string {
	if msg, _ := core.FriendlyUpstream(err); msg != "" {
		return msg
	}
	p.log.Warn("fortnite "+op+" fetch failed", zap.String("account", account), zap.Error(err))
	return "stats lookup failed"
}

// sessionStart snapshots the player's live lifetime standing for the channel.
// It fetches fresh (not through cachedLifetimeStats): the snapshot is the
// session baseline, so it must not predate the stream by a stale cache window.
func (p *api) sessionStart(ctx context.Context, req gossiprpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gossiprpc.FortniteSnapshotReply{Error: "missing account or channel"}
	}
	if msg := epicOnly(req.AccountType); msg != "" {
		return gossiprpc.FortniteSnapshotReply{Player: account, Error: msg}
	}
	stats, err := p.fetchStats(ctx, statsQuery{account: account, window: "lifetime"}, req.IsPremium)
	if err != nil {
		return gossiprpc.FortniteSnapshotReply{Player: account, Error: p.sessionError("snapshot", account, err)}
	}
	if err := p.writeSnapshot(ctx, req.ChannelID, account, stats); err != nil {
		monitor.TxnLogger(ctx, p.log).Warn("fortnite snapshot write failed", zap.String("channel_id", req.ChannelID), zap.Error(err))
		return gossiprpc.FortniteSnapshotReply{Player: stats.Player, Error: "snapshot store failed"}
	}
	return gossiprpc.FortniteSnapshotReply{Player: stats.Player}
}

// loadSnapshot reads the channel's stream-start snapshot, reporting ok only
// when one exists and tracks account — a snapshot keyed to a different account
// must not be diffed against.
func (p *api) loadSnapshot(ctx context.Context, channelID, account string) (snapshot, bool) {
	var snap snapshot
	ok, err := p.cache.GetJSON(ctx, snapshotKey(channelID), &snap)
	if err != nil {
		p.log.Warn("fortnite snapshot read failed", zap.String("channel_id", channelID), zap.Error(err))
	}
	return snap, ok && snap.Account == strings.ToLower(account)
}

// storeSnapshot writes the channel's baseline, logging the failure for callers
// that cannot surface it (the session start-tracking path).
func (p *api) storeSnapshot(ctx context.Context, channelID, account string, stats gossiprpc.FortniteStatsReply) {
	if err := p.writeSnapshot(ctx, channelID, account, stats); err != nil {
		p.log.Warn("fortnite snapshot write failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

// sessionDelta builds the session reply from the live standing and the
// stream-start snapshot. Lifetime counters only grow, but clamp defensively so
// an upstream correction can never render a negative "this stream" line;
// modeAgg.reply derives K/D and win rate exactly as the stats path does.
func sessionDelta(live gossiprpc.FortniteStatsReply, snap snapshot) gossiprpc.FortniteSessionReply {
	delta := modeAgg{
		wins:    max(0, live.Overall.Wins-snap.Wins),
		matches: max(0, live.Overall.Matches-snap.Matches),
		kills:   max(0, live.Overall.Kills-snap.Kills),
	}
	ms := delta.reply()
	return gossiprpc.FortniteSessionReply{
		Player:      live.Player,
		Wins:        ms.Wins,
		Matches:     ms.Matches,
		Kills:       ms.Kills,
		KD:          ms.KD,
		WinRate:     ms.WinRate,
		SinceUnix:   snap.AtUnix,
		HasSnapshot: true,
	}
}

// session answers the delta since the channel's stream-start snapshot. Without
// a usable snapshot (none stored, or it tracks a different account) it takes
// one now and reports HasSnapshot=false so the caller can say "tracking from
// now".
func (p *api) session(ctx context.Context, req gossiprpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gossiprpc.FortniteSessionReply{Error: "missing account or channel"}
	}
	if msg := epicOnly(req.AccountType); msg != "" {
		return gossiprpc.FortniteSessionReply{Player: account, Error: msg}
	}
	live, err := p.cachedLifetimeStats(ctx, account, req.IsPremium)
	if err != nil {
		return gossiprpc.FortniteSessionReply{Player: account, Error: p.sessionError("session", account, err)}
	}

	snap, ok := p.loadSnapshot(ctx, req.ChannelID, account)
	if !ok {
		// No baseline for this account yet: start one now so the next call diffs.
		p.storeSnapshot(ctx, req.ChannelID, account, live)
		return gossiprpc.FortniteSessionReply{Player: live.Player, SinceUnix: time.Now().Unix()}
	}
	return sessionDelta(live, snap)
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

// shopResponse is the /v2/shop envelope subset gossip reads: the rotation
// date plus the offers.
type shopResponse struct {
	Data struct {
		Date    string      `json:"date"`
		Entries []shopEntry `json:"entries"`
	} `json:"data"`
}

// shopFetch answers fortnite.shop (sesame's !store) with the current
// item-shop rotation: it spends the budget, queries /v2/shop and normalizes
// each offer to name + final price. Offers with nothing displayable are
// dropped. The shop is global, so the flow's StaticID carries no request state.
func (p *api) shopFetch(ctx context.Context, req gossiprpc.Request, _ provider.ID) (any, error) {
	if err := p.shopBucket.Enforce(ctx, p.deps.Limiter, req.IsPremium); err != nil {
		return nil, err
	}

	var resp shopResponse
	if err := p.shop.GetJSON(ctx, "/v2/shop", nil, &resp); err != nil {
		return gossiprpc.FortniteShopReply{}, err
	}

	entries := make([]gossiprpc.FortniteShopEntry, 0, len(resp.Data.Entries))
	for _, e := range resp.Data.Entries {
		name := e.displayName()
		if name == "" {
			continue
		}
		entries = append(entries, gossiprpc.FortniteShopEntry{Name: name, Price: e.FinalPrice})
	}
	// The upstream date is an ISO timestamp; the reply carries the day only.
	date, _, _ := strings.Cut(resp.Data.Date, "T")
	return gossiprpc.FortniteShopReply{Date: date, Count: len(entries), Entries: entries}, nil
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
