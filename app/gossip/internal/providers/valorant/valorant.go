// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

	rankTTL    = 5 * time.Minute
	matchesTTL = 5 * time.Minute

	accountTTL = time.Hour

	resolveTTL = 24 * time.Hour

	boardTTL = 30 * time.Minute

	bundleTTL = 6 * time.Hour

	negativeTTL = 5 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	rateWindowSeconds = 60.0

	matchCount   = 5
	boardEntries = 10
)

type Config struct {
	BaseURL          string
	ContentBaseURL   string
	APIKey           string
	RateLimit        float64
	ContentRateLimit float64
}

const providerName = "valorant"

type api struct {
	http           *core.HTTPClient
	content        *core.HTTPClient
	cache          *core.Cache
	limiter        *ratelimit.Limiter
	buckets        core.Buckets
	contentBuckets core.Buckets
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)

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

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	content := strings.TrimSuffix(cfg.ContentBaseURL, "/")
	if content == "" {
		content = defaultContentBaseURL
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 30
	}
	if cfg.ContentRateLimit <= 0 {
		cfg.ContentRateLimit = 60
	}
	return &api{
		// HenrikDev takes the raw key; a "Bearer" prefix gets 401 on every call.
		http: b.Client(base, map[string]string{
			"Authorization": cfg.APIKey,
		}, httpTimeout),
		content:        b.Client(content, nil, httpTimeout),
		cache:          d.Cache,
		limiter:        d.Limiter,
		buckets:        core.NewBuckets("ratelimit:gossip:valorant", cfg.RateLimit, rateWindowSeconds),
		contentBuckets: core.NewBuckets("ratelimit:gossip:valorant:content", cfg.ContentRateLimit, rateWindowSeconds),
	}
}

func (p *api) budget(ctx context.Context, req gossiprpc.Request) error {
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

func (p *api) shopBudget(ctx context.Context, req gossiprpc.Request) error {
	if err := p.contentBuckets.Enforce(ctx, p.limiter, req.IsPremium); err != nil {
		return err
	}
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

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

func wellFormedRiotID(name, tag string, found bool) bool {
	return found && name != "" && tag != "" && len(name) <= 32 && len(tag) <= 8
}

func (r riotIDValue) String() string { return r.name + "#" + r.tag }

func (r riotIDValue) cacheKey() string {
	return strings.ToLower(r.name) + "#" + strings.ToLower(r.tag)
}

var affinities = map[string]struct{}{
	"na": {}, "eu": {}, "ap": {}, "kr": {}, "br": {}, "latam": {},
}

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
		Key:     core.CacheID(id.cacheKey(), region, platform),
	}, ""
}

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
	return provider.ID{Display: display, Key: core.CacheID(display, region, platform)}, ""
}

type accountInfo struct {
	Puuid        string `json:"puuid"`
	Region       string `json:"region"`
	Name         string `json:"name"`
	Tag          string `json:"tag"`
	AccountLevel int    `json:"account_level"`
	Card         string `json:"card"`
	Title        string `json:"title"`
}

type accountWire struct {
	Data accountInfo `json:"data"`
}

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
		return "", &core.UpstreamError{Status: 404, Message: "could not detect region"}
	}
	return shard, nil
}

func (p *api) resolveAccount(ctx context.Context, id riotIDValue) (accountInfo, error) {
	key := core.Key(providerName, "resolve", id.cacheKey())
	return core.Cached(ctx, p.cache, key, resolveTTL, negativeTTL, nil, func(ctx context.Context) (accountInfo, error) {
		var wire accountWire
		path := "/valorant/v2/account/" + url.PathEscape(id.name) + "/" + url.PathEscape(id.tag)
		if err := p.http.GetJSON(ctx, path, nil, &wire); err != nil {
			return accountInfo{}, err
		}
		info := wire.Data
		if strings.TrimSpace(info.Puuid) == "" {
			return accountInfo{}, &core.UpstreamError{Status: 404, Message: "player not found"}
		}
		return info, nil
	})
}

func scoped(req gossiprpc.Request) (id riotIDValue, region, platform string) {
	id, _ = parseRiotID(req.Account)
	region, _ = normalizeRegion(req.Region)
	platform, _ = normalizePlatform(req.Platform)
	return id, region, platform
}

// Peak arrives as one object despite the spec's array; an array decode fails every rank lookup.
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

func peakTier(peak peakMMR) string {
	if peak.Tier.ID == 0 {
		return ""
	}
	return peak.Tier.Name
}

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

type matchEntry struct {
	Map        string  `json:"map"`
	Agent      string  `json:"agent"`
	Result     string  `json:"result"`
	Kills      int     `json:"kills"`
	Deaths     int     `json:"deaths"`
	Assists    int     `json:"assists"`
	ACS        float64 `json:"acs"`
	AgoSeconds int64   `json:"ago_seconds"`
}

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
		entry.AgoSeconds = max(int64(now.Sub(match.Metadata.StartedAt).Seconds()), 0)
		entries = append(entries, entry)
	}
	return matchesReply{
		Player:  rid.String(),
		Region:  eff,
		Matches: entries,
		Empty:   len(entries) == 0,
	}, nil
}

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

type lbEntry struct {
	Rank   int    `json:"rank"`
	Player string `json:"player"`
	Tier   int    `json:"tier"`
	RR     int    `json:"rr"`
	Wins   int    `json:"wins"`
}

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
