// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
//
// !fn (session) reads the SAME lifetime entry !fnstats fills rather than
// keeping a private copy of identical numbers — see cachedLifetimeStats. A
// channel running both commands pays one upstream round trip for both, not two.
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
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	// statsTTL matches the other stats providers' staleness budget.
	statsTTL    = 10 * time.Minute
	negativeTTL = 5 * time.Minute
	// accountTTL is how long a display-name -> account-id binding is trusted.
	// It is the single most expensive TTL in this provider, because the binding
	// sits in front of the stats call rather than beside it: fetchStats cannot
	// build the /api/v2/stats/{id} URL until the resolve has answered, so a
	// cold binding turns one upstream round trip into two SEQUENTIAL ones
	// against prod.api-fortnite.com — measured at ~700ms a call from this
	// fleet, the slowest upstream any provider here talks to. That is the whole
	// distance between the two !fnstats timings production shows: 871ms with
	// the binding warm, 1.44s with it cold.
	//
	// A day was a round number, not a sized one. Size it against what the
	// binding actually tracks and 24h is far too short: an Epic account id is
	// immutable, so this mapping can only break on a display-name change, and
	// Epic rate-limits renames to one every two weeks. Two weeks is therefore
	// the shortest interval at which a fresh answer can differ from a stale
	// one, and at 24h the provider was re-resolving an unchanged binding
	// roughly fourteen times for every time it could possibly have moved,
	// paying a 700ms serial call for each.
	//
	// Serving a stale binding through a rename is also the mild failure, not
	// the severe one: the id keeps resolving to the same account, so the stats
	// stay correct and only the canonically-cased Player name in the reply
	// lags. And it self-heals without waiting out the window — a renamed player
	// is looked up under the NEW display name, which is a different cache key
	// and resolves fresh.
	accountTTL = 14 * 24 * time.Hour
	// seasonTTL bounds how stale the auto-fetched season start may run; a
	// season rollover (4x a year) is picked up within the hour.
	seasonTTL = time.Hour
	// shopRotationHour is the hour, in UTC, at which Epic swaps the item shop.
	// The shop does not age — it is byte-identical from one swap to the next —
	// so it is cached against this boundary rather than on an interval (see
	// nextShopRotation).
	shopRotationHour = 0
	// sessionSnapshotTTL outlives any plausible single stream; Twitch caps
	// broadcasts at 48h.
	sessionSnapshotTTL = 49 * time.Hour

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second
	// seasonResolveTimeout bounds the best-effort season-start leg of a cold
	// season lookup (see resolveStatsWindow). The downstream budget picks the
	// number: sesame's !fnstats RPC gives gossip 12s (gossipRPCTimeout in
	// app/sesame/engine/gossip_rpc.go), and the mandatory /api/v2/stats call
	// that follows window resolution can itself spend httpTimeout, so
	// everything the window resolution spends past 12s - 10s = 2s comes
	// straight out of the stats call's budget. A healthy /api/v1/season
	// answers in well under half a second, so 2s is several times its real
	// cost, and exceeding it degrades the reply to lifetime — the same
	// documented fallback a failed season fetch already takes — instead of
	// holding the command open.
	seasonResolveTimeout = 2 * time.Second

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

// statsEndpoint is the subject token the byte-flow stats endpoint is declared
// under, and therefore the middle segment of its cache key. The session path
// reads that very entry (see lifetimeStatsKey), so both spell it from this one
// constant: renaming the endpoint moves the session path with it instead of
// silently splitting one shared entry back into the two duplicate fetches this
// constant exists to prevent.
const statsEndpoint = "stats"

// windowLifetime and windowSeason are the two stats windows. The session path
// pins itself to windowLifetime as a literal constant rather than routing a
// request-derived string through normalizeWindow, so no request field can ever
// steer a session read at a season entry — a season rollover mid-stream would
// corrupt a delta taken across it (see snapshot).
const (
	windowLifetime = "lifetime"
	windowSeason   = "season"
)

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
		CachedUntil(nextShopRotation, negativeTTL).
		ID(provider.StaticID("current")).
		Reply(shopErrReply).
		Budget(p.shopBudget).
		Fallback("item shop lookup failed").
		Fetch(p.shopFetch)
	if p.keyed {
		b.Endpoint(statsEndpoint).Timeout(handlerTimeout).
			Cached(statsTTL, negativeTTL).
			ID(statsID).
			Reply(statsErrReply).
			Budget(p.statsBudget).
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

// statsBudget and shopBudget spend one request's share of their upstream's
// allowance IN THAT REQUEST'S OWN LANE. They are declared on the endpoint rather
// than written inside a fetch because a fetch runs once per singleflight flight:
// a check in there is charged to whichever caller won the flight and its verdict
// is served to everyone joined to it, which let a drained standard bucket deny
// premium callers the reserve they are entitled to.
func (p *api) statsBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.statsBucket.Enforce(ctx, p.deps.Limiter, req.IsPremium)
}

func (p *api) shopBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.shopBucket.Enforce(ctx, p.deps.Limiter, req.IsPremium)
}

// statsAdmit binds the stats budget to one lane for the session path, which
// reads the shared lifetime entry through CachedBytes directly and so has no
// endpoint declaration to carry the check for it.
func (p *api) statsAdmit(isPremium bool) func(context.Context) error {
	return func(ctx context.Context) error {
		return p.statsBucket.Enforce(ctx, p.deps.Limiter, isPremium)
	}
}

// normalizeWindow maps the dashboard's window setting onto the requested
// window; whether "season" can actually be served is decided at fetch time
// (seasonStartTime), where it may degrade to lifetime.
func normalizeWindow(w string) string {
	if strings.ToLower(strings.TrimSpace(w)) == windowSeason {
		return windowSeason
	}
	return windowLifetime
}

// seasonResponse is the /api/v1/season body subset gossip reads.
type seasonResponse struct {
	SeasonDateBegin time.Time `json:"seasonDateBegin"`
}

// seasonStartTime resolves the season window's start epoch: the manual
// override when configured, otherwise the upstream's own current-season
// begin date, cached an hour so a rollover is picked up automatically. 0
// means no start could be resolved (the caller degrades to lifetime).
//
// It spends no budget of its own: every path that reaches it comes through
// fetchStats, which debits once for the whole request (see fetchStats).
func (p *api) seasonStartTime(ctx context.Context) int64 {
	if p.seasonStart > 0 {
		return p.seasonStart
	}
	key := core.Key(providerName, "season", "start")
	start, err := core.Cached(ctx, p.cache, key, seasonTTL, negativeTTL, nil, func(ctx context.Context) (int64, error) {
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
// upstream, cached for a day. An unknown name 404s and negative-caches. Like
// seasonStartTime it spends no budget of its own: fetchStats, its only caller,
// debits once for the whole request.
func (p *api) resolveAccount(ctx context.Context, account string) (accountRef, error) {
	key := core.Key(providerName, "account", strings.ToLower(account))
	return core.Cached(ctx, p.cache, key, accountTTL, negativeTTL, nil, func(ctx context.Context) (accountRef, error) {
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

// statsCacheID is the byte-flow cache identity of one stats lookup: the
// normalized window, then the case-folded account, so season and lifetime
// replies for one player never collide. It is the single definition of that
// layout — the session path derives the lifetime identity from this same
// function (lifetimeStatsKey) instead of spelling the layout out a second
// time, because two independent spellings of one key is exactly how !fn and
// !fnstats came to fetch identical numbers under two keys.
func statsCacheID(window, account string) string {
	return normalizeWindow(window) + ":" + strings.ToLower(strings.TrimSpace(account))
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
	return provider.ID{Display: a, Key: statsCacheID(req.TimeWindow, a)}, ""
}

// statsFetch answers fortnite.stats (sesame's !fnstats) with the player's
// aggregated Battle Royale stats over the requested window.
func (p *api) statsFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	q := statsQuery{account: id.Display, window: normalizeWindow(req.TimeWindow)}
	return p.fetchStats(ctx, q)
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
//
// context.WithoutCancel drops the deadline along with the cancellation — it
// returns a context whose Deadline reports no deadline and whose Done is nil
// — so the insulation alone left the season leg bounded by nothing nearer
// than httpTimeout. That is not a season-only cost: g.Wait() waits for BOTH
// legs, so a mistyped display name that 404s on the account leg in ~200ms
// still parked the caller for up to 10s on a cold season cache against a
// slow upstream. seasonResolveTimeout layers an explicit bound back over the
// insulated context to restore the missing half. The two are not the same
// knob: cancellation from the account leg is arbitrary and per-caller (one
// caller's typo), while this bound is deterministic and identical for every
// caller joined to the flight, so applying it cannot degrade one caller's
// season window on account of another's mistake.
func (p *api) resolveStatsWindow(ctx context.Context, q statsQuery) (accountRef, int64, error) {
	if q.window != windowSeason {
		ref, err := p.resolveAccount(ctx, q.account)
		return ref, 0, err
	}
	if p.seasonStart > 0 {
		ref, err := p.resolveAccount(ctx, q.account)
		return ref, p.seasonStart, err
	}

	var ref accountRef
	var season int64
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		r, err := p.resolveAccount(gctx, q.account)
		ref = r
		return err
	})
	g.Go(func() error {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(gctx), seasonResolveTimeout)
		defer cancel()
		season = p.seasonStartTime(sctx)
		return nil
	})
	if err := g.Wait(); err != nil {
		return accountRef{}, 0, err
	}
	return ref, season, nil
}

// fetchStats resolves the account and the season window, pulls the raw counter
// blob (window-filtered for season) and shapes the success reply.
//
// It spends NO budget. It cannot: every path into it runs inside a singleflight
// flight, and a budget check inside a flight is charged to whichever caller won
// the flight and its verdict is handed to everyone joined to it. That is how a
// drained standard bucket came to deny premium callers the reserve they are
// entitled to. The debit belongs to the caller, so it lives at the door: the
// stats endpoint declares Budget(p.statsBudget), the session path passes
// p.statsAdmit to CachedBytes, and sessionStart spends it inline before calling
// here. One debit per request either way, which is what the three that used to
// sit inside resolveAccount, seasonStartTime and this function amounted to
// anyway — at three Valkey round trips on a command's critical path.
func (p *api) fetchStats(ctx context.Context, q statsQuery) (gossiprpc.FortniteStatsReply, error) {
	ref, seasonStart, err := p.resolveStatsWindow(ctx, q)
	if err != nil {
		return gossiprpc.FortniteStatsReply{}, err
	}

	var query url.Values
	if q.window == windowSeason {
		if seasonStart > 0 {
			query = url.Values{"startTime": {strconv.FormatInt(seasonStart, 10)}}
		} else {
			// No season start resolvable: serve lifetime and say so.
			q.window = windowLifetime
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

// lifetimeStatsKey is the cache key the stats endpoint's LIFETIME entry lives
// at — provider token, endpoint token, statsCacheID — assembled exactly as the
// flow layer assembles it (core.Key(ref.provider, ref.endpoint, id.Key), see
// provider/flow.go). The window is the windowLifetime constant, never a
// request field, so this key cannot address a season entry.
func lifetimeStatsKey(account string) string {
	return core.Key(providerName, statsEndpoint, statsCacheID(windowLifetime, account))
}

// lifetimeEntryBuild produces the byte-flow entry for one player's lifetime
// stats: the same fetch, the same TTLs and the same friendly-error shaping the
// stats endpoint's own flow produces, so the entry is byte-identical whichever
// command fills it first.
func (p *api) lifetimeEntryBuild(account string) func(context.Context) ([]byte, time.Duration, error) {
	return func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReply(ctx, statsTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				return p.fetchStats(ctx, statsQuery{account: account, window: windowLifetime})
			},
			func(msg string) any { return statsErrReply(account, msg) },
		)
		return b, ttl, err
	}
}

// cachedLifetimeStats reads a player's live lifetime stats through THE SAME
// byte-flow entry !fnstats fills. The two commands ask the upstream the same
// question — the lifetime counter blob for one account — so they answer out of
// one entry: a warm !fnstats makes !fn free, a warm !fn makes !fnstats free,
// and a channel running both pays one /api/v2/stats call and one account
// resolve per staleness window instead of two of each.
//
// The session path is the one that pays for the sharing, and it is the right
// one to: it needs the decoded struct (it diffs the counters against the stored
// snapshot), so it eats one unmarshal of the stored bytes here. That buys the
// removal of an entire upstream round trip. The stats path pays nothing — a hit
// there still returns the stored wire bytes untouched, no envelope unmarshal
// and no reply re-marshal, which is the whole point of the byte flow.
//
// The session delta is therefore bounded by the same staleness !fnstats already
// serves (statsTTL fresh, then a stale-while-revalidate tail). The two commands
// agreeing on the numbers is worth more than the session keeping a private,
// marginally fresher copy of them.
// The reply it returns may be a SHAPED FAILURE rather than counters. The byte
// flow stores friendly failures ("player not found") as ordinary replies with a
// short TTL, so a hit can legitimately decode to a FortniteStatsReply carrying
// Error and zero counters. The caller must check Error before diffing: a zeroed
// reply run through sessionDelta would silently report a clean session for a
// player the upstream does not know.
//
// The budget stays nil here because fortnite still spends it inside fetchStats;
// when this provider moves to a declared Budget the check belongs in that
// declaration, not in this call.
func (p *api) cachedLifetimeStats(ctx context.Context, account string, isPremium bool) (gossiprpc.FortniteStatsReply, error) {
	b, err := core.CachedBytes(ctx, p.cache, lifetimeStatsKey(account), p.statsAdmit(isPremium), p.lifetimeEntryBuild(account))
	if err != nil {
		return gossiprpc.FortniteStatsReply{}, err
	}
	var live gossiprpc.FortniteStatsReply
	if err := codec.Unmarshal(b, &live); err != nil {
		return gossiprpc.FortniteStatsReply{}, err
	}
	return live, nil
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
	// Spent inline: this path deliberately bypasses the cache (the snapshot is the
	// session baseline and must not predate the stream), so there is no flow
	// declaration and no CachedBytes admission to carry the debit for it.
	if err := p.statsBudget(ctx, req); err != nil {
		return gossiprpc.FortniteSnapshotReply{Player: account, Error: p.sessionError("snapshot", account, err)}
	}
	stats, err := p.fetchStats(ctx, statsQuery{account: account, window: windowLifetime})
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
	// A shaped failure shares the entry with the counters (see cachedLifetimeStats).
	// Diffing it would report a clean session for a player that does not exist.
	if live.Error != "" {
		return gossiprpc.FortniteSessionReply{Player: account, Error: live.Error}
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

// nextShopRotation reports the next instant the item shop turns over, which is
// the only instant a cached shop reply can stop being true. Epic swaps the shop
// daily at 00:00 UTC and the payload is byte-identical in between, so this — not
// an interval — is what the shop endpoint caches against.
//
// The interval it replaces was fifteen minutes, which was wrong in both
// directions at once. Too long, because a swap could sit unnoticed for a
// quarter of an hour; and far too short, because the byte cache retains an entry
// for twice its fresh window, so the shop was gone from the cache thirty minutes
// after the last !store. For a command asked a handful of times a day that meant
// essentially every call was a full cold download — 584ms in production, 422ms
// of it upstream and 247ms of that payload transfer — to re-fetch bytes that
// provably had not changed since the last one.
//
// It is written out rather than expressed as a Truncate: time.Time.Truncate
// rounds against the zero time, which lands on midnight UTC only by coincidence
// of the calendar's origin, and the shop's boundary is too load-bearing to rest
// on that. Called exactly at a rotation it returns the FOLLOWING one, so the
// window is a full day rather than zero.
func nextShopRotation(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	rotation := time.Date(y, m, d, shopRotationHour, 0, 0, 0, time.UTC)
	if !rotation.After(now) {
		rotation = rotation.AddDate(0, 0, 1)
	}
	return rotation
}

// shopFetch answers fortnite.shop (sesame's !store) with the current
// item-shop rotation: it spends the budget, queries /v2/shop and normalizes
// each offer to name + final price. Offers with nothing displayable are
// dropped. The shop is global, so the flow's StaticID carries no request state.
func (p *api) shopFetch(ctx context.Context, _ gossiprpc.Request, _ provider.ID) (any, error) {
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
