// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package valorant exposes Riot's Valorant through the community HenrikDev API
// (api.henrikdev.xyz), joined where it matters with Riot's public content CDN
// (valorant-api.com). It answers rank/MMR, recent competitive matches,
// regional leaderboards, account lookups and the current featured bundle.
//
// Three deliberate gaps: the personal store and the night market have no
// endpoint here because both are per-account views Riot only exposes behind a
// user's own credentials, and HenrikDev explicitly bans products that collect
// those. The daily skin rotation is gone for good — Riot removed the global
// offers feed this provider would have needed (see shop.go) — so the closest
// surviving global surface, the featured bundle, is what ships.
//
// The upstream meters per API key over a rolling minute and its v4 match
// endpoints additionally bill background Riot fetches against the caller, so
// everything here reads through the shared cache aggressively: one warm entry
// answers every caller in the fleet for the TTL.
package valorant

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"ItsBagelBot/pkg/ratelimit"
)

const (
	defaultBaseURL        = "https://api.henrikdev.xyz"
	defaultContentBaseURL = "https://valorant-api.com"

	// Rank and matches change per game played; two minutes keeps an RR delta
	// honest across a session without letting a squad of viewers fan out into
	// upstream calls (they collapse onto one flight instead). Clash Royale's
	// five minutes fits a profile that moves daily; a competitive ladder that
	// moves per match does not.
	rankTTL    = 2 * time.Minute
	matchesTTL = 2 * time.Minute

	// The account reply carries slow-drifting cosmetics (level, card, title);
	// an hour of staleness is invisible next to how often any of it changes.
	accountTTL = time.Hour

	// The resolve leg backs auto-region detection. PUUID and shard are
	// immutable for an account's life, so this is cached a full day — the
	// point is that after one caller pays for detection, nobody in the fleet
	// pays again for 24 hours. Renames do go stale inside that window, but
	// nothing rendered from this entry comes out of it (replies echo the
	// caller's input); only the region rides forward.
	resolveTTL = 24 * time.Hour

	// HenrikDev rebuilds leaderboards roughly hourly (the payload says so via
	// last_update/next_update); thirty minutes is half that cadence, so a
	// served board is at worst one rebuild behind and stale-while-revalidate
	// absorbs the boundary.
	boardTTL = 30 * time.Minute

	// The featured bundle turns over on Riot's staggered store schedule with
	// no published UTC boundary, and its payload carries an exact remaining-
	// seconds countdown that renders fresher than any window we could derive.
	// Six hours keeps a served copy within a rounding error of that schedule
	// at ~4 upstream reads per day. The old offers feed had a hard 03:00 UTC
	// boundary worth pinning CachedUntil to; Riot removed it entirely (see
	// shop.go), so the deadline machinery has nothing real to pin to here.
	bundleTTL = 6 * time.Hour

	negativeTTL = 5 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	// RateLimit is configured as requests per minute.
	rateWindowSeconds = 60.0

	// How many recent competitive matches a lookup reports and how deep the
	// leaderboard slice goes. Both are chat-render sized: five results fill a
	// message without truncation, ten names fit a leaderboard post.
	matchCount   = 5
	boardEntries = 10
)

// Config carries both upstream hosts and their per-minute budgets. APIKey must
// be non-empty; providers.All skips this provider otherwise. The content CDN
// needs no credential of its own — it exists to turn the store payload's item
// UUIDs into names, icons and rarity colours.
type Config struct {
	BaseURL          string
	ContentBaseURL   string
	APIKey           string
	RateLimit        float64
	ContentRateLimit float64
}

// providerName is the subject token this provider answers under.
const providerName = "valorant"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	// http dials HenrikDev; content dials the keyless Riot content CDN.
	http    *core.HTTPClient
	content *core.HTTPClient
	cache   *core.Cache
	limiter *ratelimit.Limiter
	// buckets spends the HenrikDev key allowance; contentBuckets spends the
	// CDN's. They are separate budgets because they meter separately: the key
	// is per-account, the CDN meters per source IP fleet-wide.
	buckets        core.Buckets
	contentBuckets core.Buckets
}

// New builds the Valorant provider: four keyed byte-flow views over HenrikDev
// plus the featured-bundle viewer, which joins HenrikDev's store payload
// against the content CDN's catalogue.
func New(cfg Config, d provider.Deps) provider.Provider {
	p := newAPI(cfg, d)
	b := provider.NewProvider(providerName, d)

	b.Endpoint("rank").Timeout(handlerTimeout).
		Cached(rankTTL, negativeTTL).
		ID(riotID).
		Reply(func(id, msg string) any { return rankReply{Player: id, Error: msg} }).
		Budget(p.budget).
		Fallback("rank lookup failed").
		Fetch(p.rankFetch)

	b.Endpoint("matches").Timeout(handlerTimeout).
		Cached(matchesTTL, negativeTTL).
		ID(riotID).
		Reply(func(id, msg string) any { return matchesReply{Player: id, Error: msg} }).
		Budget(p.budget).
		Fallback("match history lookup failed").
		Fetch(p.matchesFetch)

	b.Endpoint("account").Timeout(handlerTimeout).
		Cached(accountTTL, negativeTTL).
		ID(riotID).
		Reply(func(id, msg string) any { return accountReply{Player: id, Error: msg} }).
		Budget(p.budget).
		Fallback("account lookup failed").
		Fetch(p.accountFetch)

	b.Endpoint("leaderboard").Timeout(handlerTimeout).
		Cached(boardTTL, negativeTTL).
		ID(boardID).
		Reply(func(id, msg string) any { return leaderboardReply{Player: id, Error: msg} }).
		Budget(p.budget).
		Fallback("leaderboard lookup failed").
		Fetch(p.boardFetch)

	b.Endpoint("shop").Timeout(handlerTimeout).
		Cached(bundleTTL, negativeTTL).
		ID(provider.StaticID("featured")).
		Reply(func(_, msg string) any { return shopReply{Error: msg} }).
		Budget(p.shopBudget).
		Fallback("shop lookup failed").
		Fetch(p.shopFetch)

	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	content := strings.TrimSuffix(cfg.ContentBaseURL, "/")
	if content == "" {
		content = defaultContentBaseURL
	}
	// HenrikDev's Basic tier allows 30 requests/min and that is what the
	// fleet runs; the default sits AT the ceiling so bursts deny locally
	// instead of tripping the upstream limiter (a 429 there costs more than a
	// local deny — it poisons the cache fill). An Enhanced key (90/min) must
	// raise VALORANT_RATE_LIMIT to match, or the paid headroom is wasted.
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 30
	}
	// valorant-api.com publishes no hard limit; 60/min is conservative for a
	// payload this size, and the skins catalogue caches for a day anyway.
	if cfg.ContentRateLimit <= 0 {
		cfg.ContentRateLimit = 60
	}
	return &api{
		// HenrikDev takes the raw key in Authorization — no "Bearer" prefix.
		// Its auth scheme is a bare token (sending Bearer yields 401), unlike
		// the Supercell proxy clashroyale uses.
		http: core.NewHTTPClient(base, map[string]string{
			"Authorization": cfg.APIKey,
		}, httpTimeout),
		content:        core.NewHTTPClient(content, nil, httpTimeout),
		cache:          d.Cache,
		limiter:        d.Limiter,
		buckets:        core.NewBuckets("ratelimit:gossip:valorant", cfg.RateLimit, rateWindowSeconds),
		contentBuckets: core.NewBuckets("ratelimit:gossip:valorant:content", cfg.ContentRateLimit, rateWindowSeconds),
	}
}

// budget spends one request's share of the HenrikDev allowance in that
// request's own lane. Every endpoint shares one allowance because every call
// bills the same key, so it is declared on each endpoint rather than written
// inside any fetch: a check there runs once per singleflight flight, is
// charged to whichever caller won it, and hands that verdict to everyone
// joined — which let a drained standard bucket deny premium callers the
// reserve they are entitled to.
func (p *api) budget(ctx context.Context, req gossiprpc.Request) error {
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

// shopBudget debits both upstreams the shop leg touches, in call order:
// content first (the likely-cached catalogue read), then the priced offers.
func (p *api) shopBudget(ctx context.Context, req gossiprpc.Request) error {
	if err := p.contentBuckets.Enforce(ctx, p.limiter, req.IsPremium); err != nil {
		return err
	}
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

// riotIDValue is the canonical Riot ID split at "#". Tags are case-insensitive
// per Riot but conventionally uppercase; the display form preserves the name
// exactly as typed while the cache key folds both halves lowercased, so
// "Frosty#EUW1" and "frosty#euw1" share one entry.
type riotIDValue struct {
	name string
	tag  string
}

func parseRiotID(account string) (riotIDValue, string) {
	raw := strings.TrimSpace(account)
	name, tag, found := strings.Cut(raw, "#")
	name, tag = strings.TrimSpace(name), strings.TrimSpace(tag)
	if !wellFormedRiotID(name, tag, found) {
		return riotIDValue{}, "invalid riot id (want name#tag)"
	}
	return riotIDValue{name: name, tag: strings.ToUpper(tag)}, ""
}

// wellFormedRiotID carries every fault condition a split Riot ID can have so
// parseRiotID spends one branch on them. The bounds mirror Riot's own limits
// (16-char name, 3-5 char tag) with slack; anything past these cannot exist,
// so it is rejected before spending a cache slot or an upstream call on it.
func wellFormedRiotID(name, tag string, found bool) bool {
	return found && name != "" && tag != "" && len(name) <= 32 && len(tag) <= 8
}

func (r riotIDValue) String() string { return r.name + "#" + r.tag }

func (r riotIDValue) cacheKey() string {
	return strings.ToLower(r.name) + "#" + strings.ToLower(r.tag)
}

// affinities are the Valorant shards HenrikDev serves. There is no "es": Spain
// players belong to eu, despite what old bot folklore says.
var affinities = map[string]struct{}{
	"na": {}, "eu": {}, "ap": {}, "kr": {}, "br": {}, "latam": {},
}

// normalizeRegion maps the caller's region to its canonical lowercase form.
// Empty means "detect it from the account", returned as the sentinel "auto".
func normalizeRegion(region string) (string, string) {
	r := strings.ToLower(strings.TrimSpace(region))
	if r == "" {
		return "auto", ""
	}
	if _, ok := affinities[r]; ok {
		return r, ""
	}
	return "", "unknown region (want na, eu, ap, kr, br or latam)"
}

// normalizePlatform maps the ladder split; pc is the default because it is
// where the overwhelmingly larger player base sits.
func normalizePlatform(platform string) (string, string) {
	switch p := strings.ToLower(strings.TrimSpace(platform)); p {
	case "":
		return "pc", ""
	case "pc", "console":
		return p, ""
	default:
		return "", "unknown platform (want pc or console)"
	}
}

// riotID validates the account plus its scoping inputs and keys the cache on
// all three: the same Riot ID answers differently per region (and per
// platform), so the key must fold them even when the caller left them unset —
// folding the normalized defaults rather than raw input means "na" and "NA"
// share one entry, and unset-vs-"pc" do too.
func riotID(req gossiprpc.Request) (provider.ID, string) {
	id, msg := parseRiotID(req.Account)
	if msg != "" {
		return provider.ID{Display: strings.TrimSpace(req.Account)}, msg
	}
	region, msg := normalizeRegion(req.Region)
	if msg != "" {
		return provider.ID{Display: id.String()}, msg
	}
	platform, msg := normalizePlatform(req.Platform)
	if msg != "" {
		return provider.ID{Display: id.String()}, msg
	}
	return provider.ID{
		Display: id.String(),
		Key:     id.cacheKey() + ":" + region + ":" + platform,
	}, ""
}

// boardID scopes a leaderboard request. Unlike player endpoints, the account
// here is optional — a bare top-10 ask has no Riot ID — but then the region
// must be explicit, since there is nothing to detect it from.
func boardID(req gossiprpc.Request) (provider.ID, string) {
	display := strings.TrimSpace(req.Account)
	region, msg := normalizeRegion(req.Region)
	if msg != "" {
		return provider.ID{}, msg
	}
	platform, msg := normalizePlatform(req.Platform)
	if msg != "" {
		return provider.ID{}, msg
	}
	if display == "" && region == "auto" {
		return provider.ID{}, "missing region (no account to detect it from)"
	}
	return provider.ID{Display: display, Key: strings.ToLower(display) + ":" + region + ":" + platform}, ""
}

// accountInfo is the v2 account subset every consumer needs. Region comes back
// normalized from the upstream already ("na", not "NORTH AMERICA"); it is
// lowercased again defensively because the whole auto-detect path keys off it.
//
// This one struct feeds both the public account endpoint and the identity
// resolve behind auto-region, deliberately: they hit the same upstream path,
// and keeping one wire type means a shape change upstream breaks loudly in one
// place instead of silently diverging between two.
type accountInfo struct {
	Puuid        string `json:"puuid"`
	Region       string `json:"region"`
	Name         string `json:"name"`
	Tag          string `json:"tag"`
	AccountLevel int    `json:"account_level"`
	Card         string `json:"card"`
	Title        string `json:"title"`
}

// accountWire is HenrikDev's response envelope: every reply is {"status",
// "data"}, unlike RoyaleAPI's bare objects.
type accountWire struct {
	Data accountInfo `json:"data"`
}

// effectiveRegion returns the affinity a lookup must run against: the
// caller's explicit one, or the account's own shard resolved through the
// shared day-long identity entry. Auto costs one extra upstream read on a cold
// cache and none afterwards, which is what makes region-less commands viable
// at all.
func (p *api) effectiveRegion(ctx context.Context, id riotIDValue, region string) (string, error) {
	if region != "auto" {
		return region, nil
	}
	info, err := p.resolveAccount(ctx, id)
	if err != nil {
		return "", err
	}
	shard := strings.ToLower(strings.TrimSpace(info.Region))
	if shard == "" {
		// Observed for brand-new accounts before their first game; without
		// this guard the lookup would proceed as "/mmr//" and fail opaquely.
		return "", &core.UpstreamError{Status: 404, Message: "could not detect region"}
	}
	return shard, nil
}

// resolveAccount reads the identity entry backing auto-region. Only
// successes cache positively, and only genuine misses reach the upstream: the
// day-long TTL is the whole economy of the feature.
func (p *api) resolveAccount(ctx context.Context, id riotIDValue) (accountInfo, error) {
	key := core.Key(providerName, "resolve", id.cacheKey())
	return core.Cached(ctx, p.cache, key, resolveTTL, negativeTTL, nil, func(ctx context.Context) (accountInfo, error) {
		var wire accountWire
		path := "/valorant/v2/account/" + url.PathEscape(id.name) + "/" + url.PathEscape(id.tag)
		if err := p.http.GetJSON(ctx, path, nil, &wire); err != nil {
			return accountInfo{}, err
		}
		info := wire.Data
		// A 200 with no puuid happens when Riot's index has not settled on a
		// freshly renamed account; minting the 404 here lets negative caching
		// absorb the retry storm that would otherwise follow.
		if strings.TrimSpace(info.Puuid) == "" {
			return accountInfo{}, &core.UpstreamError{Status: 404, Message: "player not found"}
		}
		return info, nil
	})
}

// scoped unpacks what riotID validated. The ID funcs may not thread their
// parsed values through provider.ID beyond Display/Key, so fetches re-run the
// normalizers on the request — cheap pure functions whose verdicts are already
// known good by the time a fetch runs.
func scoped(req gossiprpc.Request) (id riotIDValue, region, platform string) {
	id, _ = parseRiotID(req.Account)
	region, _ = normalizeRegion(req.Region)
	platform, _ = normalizePlatform(req.Platform)
	return id, region, platform
}

// --- rank -------------------------------------------------------------------

// mmrWire covers HenrikDev MMR v3. Peak arrives here as ONE object — the
// account's highest recorded seasonal standing — though the published OpenAPI
// spec documents an array; the live payload wins (verified against a Radiant
// account, where an array-typed decode failed and cost every rank lookup).
type mmrWire struct {
	Data mmrData `json:"data"`
}

type mmrData struct {
	Current currentMMR `json:"current"`
	Peak    peakMMR    `json:"peak"`
}

type currentMMR struct {
	Elo        int       `json:"elo"`
	RR         int       `json:"rr"`
	LastChange int       `json:"last_change"`
	Tier       tierCombo `json:"tier"`
	Placement  placement `json:"leaderboard_placement"`
}

type tierCombo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type placement struct {
	Rank int `json:"rank"`
}

type peakMMR struct {
	Tier tierCombo `json:"tier"`
	RR   int       `json:"rr"`
}

// rankReply is the answer to valorant.rank: the standing a viewer asks about
// first, and the reason this provider exists. LastChange is the RR delta of
// the most recent competitive game (negative on a loss) — the single number
// players screenshot most. Placement is the current act leaderboard position,
// 0 when unplaced. Tier ids are 0 for unranked accounts, which drives
// Unranked; Elo still arrives for them but is meaningless, so it is zeroed
// alongside to keep templates from printing "-23 elo, Unranked".
type rankReply struct {
	Player     string `json:"player"`
	Region     string `json:"region"`
	Tier       string `json:"tier"`
	Elo        int    `json:"elo"`
	RR         int    `json:"rr"`
	LastChange int    `json:"last_change"`
	PeakTier   string `json:"peak_tier"`
	Placement  int    `json:"placement"`
	Unranked   bool   `json:"unranked"`
	Error      string `json:"error,omitempty"`
}

func (p *api) rankFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	rid, region, platform := scoped(req)
	eff, err := p.effectiveRegion(ctx, rid, region)
	if err != nil {
		return nil, err
	}
	var wire mmrWire
	path := "/valorant/v3/mmr/" + eff + "/" + platform + "/" + url.PathEscape(rid.name) + "/" + url.PathEscape(rid.tag)
	if err := p.http.GetJSON(ctx, path, nil, &wire); err != nil {
		return nil, err
	}
	mmr := wire.Data
	reply := rankReply{
		Player:     rid.String(),
		Region:     eff,
		Tier:       mmr.Current.Tier.Name,
		RR:         mmr.Current.RR,
		LastChange: mmr.Current.LastChange,
		PeakTier:   peakTier(mmr.Peak),
		Placement:  mmr.Current.Placement.Rank,
		Unranked:   mmr.Current.Tier.ID == 0,
	}
	if !reply.Unranked {
		reply.Elo = mmr.Current.Elo
	}
	return reply, nil
}

// peakTier reads the account's all-time peak standing. Tier id 0 (unplaced in
// any recorded season) renders as empty rather than the upstream's placeholder
// name, so templates can omit the line entirely.
func peakTier(peak peakMMR) string {
	if peak.Tier.ID == 0 {
		return ""
	}
	return peak.Tier.Name
}

// --- matches ----------------------------------------------------------------

// matchesWire covers Matches v4 list entries. The full payload includes rounds
// and kills arrays used by deep-analysis tools; neither survives decoding
// here because chat wants summaries, and dropping them keeps a five-match
// decode allocation small.
type matchesWire struct {
	Data []matchV4 `json:"data"`
}

type matchV4 struct {
	Metadata matchMeta     `json:"metadata"`
	Players  []matchPlayer `json:"players"`
	Teams    []matchTeam   `json:"teams"`
}

type matchMeta struct {
	Map         mapCombo  `json:"map"`
	StartedAt   time.Time `json:"started_at"`
	IsCompleted bool      `json:"is_completed"`
}

type mapCombo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type matchPlayer struct {
	Puuid      string     `json:"puuid"`
	Name       string     `json:"name"`
	Tag        string     `json:"tag"`
	TeamID     string     `json:"team_id"`
	Agent      agentCombo `json:"agent"`
	Statistics matchStats `json:"stats"`
}

type agentCombo struct {
	Name string `json:"name"`
}

type matchStats struct {
	Kills   int `json:"kills"`
	Deaths  int `json:"deaths"`
	Assists int `json:"assists"`
	Score   int `json:"score"`
}

type matchTeam struct {
	TeamID string     `json:"team_id"`
	Won    bool       `json:"won"`
	Rounds teamRounds `json:"rounds"`
}

type teamRounds struct {
	Won  int `json:"won"`
	Lost int `json:"lost"`
}

// matchEntry summarizes one game for a chat line. ACS is computed here rather
// than echoed because the upstream ships a per-round score total, not a rate:
// score divided by the player's team rounds, rounded to a whole number the way
// trackers render it.
type matchEntry struct {
	Map        string  `json:"map"`
	Agent      string  `json:"agent"`
	Result     string  `json:"result"` // "win" | "loss" | "draw"
	Kills      int     `json:"kills"`
	Deaths     int     `json:"deaths"`
	Assists    int     `json:"assists"`
	ACS        float64 `json:"acs"`
	AgoSeconds int64   `json:"ago_seconds"`
}

// matchesReply is the answer to valorant.matches: the last few completed
// competitive games, newest first. Incomplete games (someone dodged in lobby)
// are skipped rather than shown as ghost rows. Empty is true when the account
// simply has no ranked games in the upstream's retained window — a normal
// answer, not an error.
type matchesReply struct {
	Player  string       `json:"player"`
	Region  string       `json:"region"`
	Matches []matchEntry `json:"matches"`
	Empty   bool         `json:"empty"`
	Error   string       `json:"error,omitempty"`
}

func (p *api) matchesFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	rid, region, platform := scoped(req)
	eff, err := p.effectiveRegion(ctx, rid, region)
	if err != nil {
		return nil, err
	}
	var history matchesWire
	path := "/valorant/v4/matches/" + eff + "/" + platform + "/" + url.PathEscape(rid.name) + "/" + url.PathEscape(rid.tag)
	query := url.Values{"mode": {"competitive"}, "size": {strconv.Itoa(matchCount)}}
	if err := p.http.GetJSON(ctx, path, query, &history); err != nil {
		return nil, err
	}
	now := time.Now()
	entries := make([]matchEntry, 0, len(history.Data))
	for _, match := range history.Data {
		if !match.Metadata.IsCompleted {
			continue
		}
		player, ok := findSelf(match.Players, rid)
		if !ok {
			// The queried player must appear in their own match list; a miss
			// means Riot's name index lags a rename, and rendering a rowless
			// shell would lie about the game existing.
			continue
		}
		entry := matchEntry{
			Map:     match.Metadata.Map.Name,
			Agent:   player.Agent.Name,
			Result:  matchResult(match.Teams, player.TeamID),
			Kills:   player.Statistics.Kills,
			Deaths:  player.Statistics.Deaths,
			Assists: player.Statistics.Assists,
		}
		if rounds := teamRoundsPlayed(match.Teams, player.TeamID); rounds > 0 {
			entry.ACS = float64(player.Statistics.Score) / float64(rounds)
		}
		entry.ACS = float64(int(entry.ACS*10+0.5)) / 10
		entry.AgoSeconds = int64(now.Sub(match.Metadata.StartedAt).Seconds())
		if entry.AgoSeconds < 0 {
			entry.AgoSeconds = 0
		}
		entries = append(entries, entry)
	}
	return matchesReply{
		Player:  rid.String(),
		Region:  eff,
		Matches: entries,
		Empty:   len(entries) == 0,
	}, nil
}

// findSelf locates the queried player among the ten in a match. Name+tag is
// the join key because fetching by puuid would force the resolve leg on every
// matches call; Riot IDs are unique enough for the player's own history.
func findSelf(players []matchPlayer, rid riotIDValue) (matchPlayer, bool) {
	for _, player := range players {
		if strings.EqualFold(player.Name, rid.name) && strings.EqualFold(player.Tag, rid.tag) {
			return player, true
		}
	}
	return matchPlayer{}, false
}

func teamRoundsPlayed(teams []matchTeam, teamID string) int {
	for _, team := range teams {
		if team.TeamID == teamID {
			return team.Rounds.Won + team.Rounds.Lost
		}
	}
	return 0
}

// matchResult classifies from the won flags alone: a side that won is a win,
// a loss is any other side having won, and neither flag set (forfeits resolved
// as draws, rare) falls through to draw rather than guessing.
func matchResult(teams []matchTeam, teamID string) string {
	mine, theirs := false, false
	for _, team := range teams {
		if !team.Won {
			continue
		}
		if team.TeamID == teamID {
			mine = true
		} else {
			theirs = true
		}
	}
	switch {
	case mine:
		return "win"
	case theirs:
		return "loss"
	default:
		return "draw"
	}
}

// --- account ----------------------------------------------------------------

// accountReply is the answer to valorant.account: who a Riot ID resolves to,
// plus the level/card/title flex. It is also the cheapest correctness probe —
// a caller unsure of spelling or region gets one definitive answer here.
type accountReply struct {
	Player       string `json:"player"`
	Puuid        string `json:"puuuid,omitempty"`
	Region       string `json:"region,omitempty"`
	AccountLevel int    `json:"account_level,omitempty"`
	Card         string `json:"card,omitempty"`
	Title        string `json:"title,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (p *api) accountFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	rid, _, _ := scoped(req)
	var wire accountWire
	path := "/valorant/v2/account/" + url.PathEscape(rid.name) + "/" + url.PathEscape(rid.tag)
	if err := p.http.GetJSON(ctx, path, nil, &wire); err != nil {
		return nil, err
	}
	info := wire.Data
	// Same 200-but-empty mint as resolveAccount: renames mid-propagation come
	// back complete-looking but hollow.
	if strings.TrimSpace(info.Puuid) == "" {
		return nil, &core.UpstreamError{Status: 404, Message: "player not found"}
	}
	return accountReply{
		Player:       rid.String(),
		Puuid:        info.Puuid,
		Region:       strings.ToLower(info.Region),
		AccountLevel: info.AccountLevel,
		Card:         info.Card,
		Title:        info.Title,
	}, nil
}

// --- leaderboard ------------------------------------------------------------

// boardWire covers Leaderboard v3, the platform-aware variant. V2 predates
// console splits; asking it for a console board silently returns PC data,
// which is exactly the kind of quiet wrongness the extra version segment buys
// out of.
type boardWire struct {
	Data struct {
		Players []boardPlayer `json:"players"`
	} `json:"data"`
}

type boardPlayer struct {
	LeaderboardRank int    `json:"leaderboard_rank"`
	Name            string `json:"name"`
	Tag             string `json:"tag"`
	Wins            int    `json:"wins"`
	RR              int    `json:"rr"`
	Tier            int    `json:"tier"`
}

// lbEntry is one ranked row. Tier stays the numeric competitive tier id rather
// than a spelled-out name: the module rendering this owns a static id→icon
// table already (every Valorant tracker does), and threading names through
// would mean a second content-CDN join per board for no gain.
type lbEntry struct {
	Rank   int    `json:"rank"`
	Player string `json:"player"`
	Tier   int    `json:"tier"`
	RR     int    `json:"rr"`
	Wins   int    `json:"wins"`
}

// leaderboardReply is the answer to valorant.leaderboard: the top slice of one
// regional board. Board echoes "<region>/<platform>" so a template can say
// which board it printed; Player echoes the account scoping it when one was
// given ("" for a bare top-N ask). Anonymized rows arrive with placeholder
// names from the upstream itself and are passed through as-is — hiding them
// would shift everyone's rank and lie about the board's composition.
type leaderboardReply struct {
	Player  string    `json:"player,omitempty"`
	Board   string    `json:"board,omitempty"`
	Entries []lbEntry `json:"entries"`
	Empty   bool      `json:"empty"`
	Error   string    `json:"error,omitempty"`
}

func (p *api) boardFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	_, region, platform := scoped(req)
	eff := region
	if region == "auto" {
		// boardID guarantees an account exists whenever region is auto.
		rid, _, _ := scoped(req)
		resolved, err := p.effectiveRegion(ctx, rid, region)
		if err != nil {
			return nil, err
		}
		eff = resolved
	}
	var board boardWire
	path := "/valorant/v3/leaderboard/" + eff + "/" + platform
	if err := p.http.GetJSON(ctx, path, nil, &board); err != nil {
		return nil, err
	}
	players := board.Data.Players
	sort.SliceStable(players, func(i, j int) bool {
		return players[i].LeaderboardRank < players[j].LeaderboardRank
	})
	if len(players) > boardEntries {
		players = players[:boardEntries]
	}
	entries := make([]lbEntry, 0, len(players))
	for _, player := range players {
		entries = append(entries, lbEntry{
			Rank:   player.LeaderboardRank,
			Player: player.Name + "#" + player.Tag,
			Tier:   player.Tier,
			RR:     player.RR,
			Wins:   player.Wins,
		})
	}
	return leaderboardReply{
		Player:  id.Display,
		Board:   eff + "/" + platform,
		Entries: entries,
		Empty:   len(entries) == 0,
	}, nil
}
