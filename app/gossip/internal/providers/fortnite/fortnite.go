// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	statsTTL           = 10 * time.Minute
	negativeTTL        = 5 * time.Minute
	accountTTL         = 14 * 24 * time.Hour
	seasonTTL          = time.Hour
	shopRotationHour   = 0
	sessionSnapshotTTL = 49 * time.Hour

	httpTimeout          = 10 * time.Second
	handlerTimeout       = 15 * time.Second
	seasonResolveTimeout = 2 * time.Second

	shopWindowSeconds  = 60.0
	statsWindowSeconds = 86400.0
)

type Config struct {
	ShopBaseURL     string
	StatsBaseURL    string
	APIKey          string
	ShopRateLimit   float64
	StatsRateLimit  float64
	SeasonStartUnix int64
}

const providerName = "fortnite"

const statsEndpoint = "stats"

const (
	windowLifetime = "lifetime"
	windowSeason   = "season"
)

type api struct {
	shop  *core.HTTPClient
	stats *core.HTTPClient
	cache *core.Cache
	log   *zap.Logger

	deps        provider.Deps
	shopBucket  core.Buckets
	statsBucket core.Buckets
	seasonStart int64
	keyed       bool
	bldr        *provider.Builder
	built       provider.Provider
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	return newAPI(cfg, d, b).build()
}

func (p *api) build() provider.Provider {
	if p.built != nil {
		return p.built
	}
	b := p.bldr
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
		b.Endpoint("session_end").Timeout(handlerTimeout).Handle(p.sessionEnd)
	}
	p.built = b.Build()
	return p.built
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
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
		shop:        b.Client(shopBase, nil, httpTimeout),
		stats:       b.Client(statsBase, statsHeaders, httpTimeout),
		cache:       d.Cache,
		log:         d.Logger(),
		deps:        d,
		shopBucket:  core.NewBuckets("ratelimit:gossip:fortnite", cfg.ShopRateLimit, shopWindowSeconds),
		statsBucket: core.NewBuckets("ratelimit:gossip:fortnite:stats", cfg.StatsRateLimit, statsWindowSeconds),
		seasonStart: cfg.SeasonStartUnix,
		keyed:       cfg.APIKey != "",
		bldr:        b,
	}
}

func shopErrReply(_, msg string) any { return gossiprpc.FortniteShopReply{Error: msg} }
func statsErrReply(id, msg string) any {
	return gossiprpc.FortniteStatsReply{Player: id, Error: msg}
}

func (p *api) statsBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.statsBucket.Enforce(ctx, p.deps.Limiter, req.IsPremium)
}

func (p *api) shopBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.shopBucket.Enforce(ctx, p.deps.Limiter, req.IsPremium)
}

func (p *api) statsAdmit(isPremium bool) func(context.Context) error {
	return func(ctx context.Context) error {
		return p.statsBucket.Enforce(ctx, p.deps.Limiter, isPremium)
	}
}

func normalizeWindow(w string) string {
	if strings.ToLower(strings.TrimSpace(w)) == windowSeason {
		return windowSeason
	}
	return windowLifetime
}

type seasonResponse struct {
	SeasonDateBegin time.Time `json:"seasonDateBegin"`
}

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

func epicOnly(accountType string) string {
	switch strings.ToLower(strings.TrimSpace(accountType)) {
	case "", "epic":
		return ""
	default:
		return "only Epic display names are supported right now"
	}
}

type accountRef struct {
	ID   string `json:"id"`
	Name string `json:"displayName"`
}

func friendly404(err error, msg string) error {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && ue.Status == 404 {
		return &core.UpstreamError{Status: 404, Message: msg}
	}
	return err
}

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

type modeAgg struct {
	wins, matches, kills int64
}

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

type statsQuery struct {
	account string
	window  string
}

func statsCacheID(window, account string) string {
	return core.CacheID(normalizeWindow(window), account)
}

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

func (p *api) statsFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	q := statsQuery{account: id.Display, window: normalizeWindow(req.TimeWindow)}
	return p.fetchStats(ctx, q)
}

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

type snapshot struct {
	Account string `json:"account"`
	Player  string `json:"player"`
	Wins    int64  `json:"wins"`
	Matches int64  `json:"matches"`
	Kills   int64  `json:"kills"`
	AtUnix  int64  `json:"at_unix"`
}

func snapshotKey(channelID string) string { return core.Key("fortnite", "session", channelID) }

func lifetimeStatsKey(account string) string {
	return core.Key(providerName, statsEndpoint, statsCacheID(windowLifetime, account))
}

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

func (p *api) sessionError(op, account string, err error) string {
	if msg, _ := core.FriendlyUpstream(err); msg != "" {
		return msg
	}
	p.log.Warn("fortnite "+op+" fetch failed", zap.String("account", account), zap.Error(err))
	return "stats lookup failed"
}

func (p *api) sessionStart(ctx context.Context, req gossiprpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gossiprpc.FortniteSnapshotReply{Error: "missing account or channel"}
	}
	if msg := epicOnly(req.AccountType); msg != "" {
		return gossiprpc.FortniteSnapshotReply{Player: account, Error: msg}
	}
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

func (p *api) sessionEnd(ctx context.Context, req gossiprpc.Request) any {
	if req.ChannelID == "" {
		return gossiprpc.FortniteSnapshotReply{Error: "missing channel"}
	}
	if err := p.cache.DelJSON(ctx, snapshotKey(req.ChannelID)); err != nil {
		return gossiprpc.FortniteSnapshotReply{Error: "snapshot delete failed"}
	}
	return gossiprpc.FortniteSnapshotReply{}
}

func (p *api) loadSnapshot(ctx context.Context, channelID, account string) (snapshot, bool) {
	var snap snapshot
	ok, err := p.cache.GetJSON(ctx, snapshotKey(channelID), &snap)
	if err != nil {
		p.log.Warn("fortnite snapshot read failed", zap.String("channel_id", channelID), zap.Error(err))
	}
	return snap, ok && snap.Account == strings.ToLower(account)
}

func (p *api) storeSnapshot(ctx context.Context, channelID, account string, stats gossiprpc.FortniteStatsReply) {
	if err := p.writeSnapshot(ctx, channelID, account, stats); err != nil {
		p.log.Warn("fortnite snapshot write failed", zap.String("channel_id", channelID), zap.Error(err))
	}
}

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
	if live.Error != "" {
		return gossiprpc.FortniteSessionReply{Player: account, Error: live.Error}
	}

	snap, ok := p.loadSnapshot(ctx, req.ChannelID, account)
	if !ok {
		p.storeSnapshot(ctx, req.ChannelID, account, live)
		return gossiprpc.FortniteSessionReply{Player: live.Player, SinceUnix: time.Now().Unix()}
	}
	return sessionDelta(live, snap)
}

type named struct {
	Name string `json:"name"`
}

type titled struct {
	Title string `json:"title"`
}

type shopEntry struct {
	FinalPrice  int64    `json:"finalPrice"`
	Bundle      *named   `json:"bundle"`
	BrItems     []named  `json:"brItems"`
	Instruments []named  `json:"instruments"`
	Cars        []named  `json:"cars"`
	LegoKits    []named  `json:"legoKits"`
	Tracks      []titled `json:"tracks"`
}

type shopResponse struct {
	Data struct {
		Date    string      `json:"date"`
		Entries []shopEntry `json:"entries"`
	} `json:"data"`
}

func nextShopRotation(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	rotation := time.Date(y, m, d, shopRotationHour, 0, 0, 0, time.UTC)
	if !rotation.After(now) {
		rotation = rotation.AddDate(0, 0, 1)
	}
	return rotation
}

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
	date, _, _ := strings.Cut(resp.Data.Date, "T")
	return gossiprpc.FortniteShopReply{Date: date, Count: len(entries), Entries: entries}, nil
}

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
