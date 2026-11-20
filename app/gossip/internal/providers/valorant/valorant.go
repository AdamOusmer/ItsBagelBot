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

	"go.uber.org/zap"
)

const (
	defaultBaseURL        = "https://api.henrikdev.xyz"
	defaultContentBaseURL = "https://valorant-api.com"

	rankTTL    = 5 * time.Minute
	recordTTL  = 5 * time.Minute
	matchesTTL = 5 * time.Minute

	accountTTL = 5 * time.Minute

	linkTTL = time.Minute

	boardTTL = 30 * time.Minute

	negativeTTL = 5 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	rateWindowSeconds = 60.0

	matchCount   = 5
	boardEntries = 10
)

type Config struct {
	BaseURL        string
	ContentBaseURL string
	APIKey         string
	RateLimit      float64
	Riot           RiotConfig
}

const providerName = "valorant"

type api struct {
	http    *core.HTTPClient
	content *core.HTTPClient
	riot    *riotAPI
	links   provider.RiotLinkResolver
	log     *zap.Logger
	cache   *core.Cache
	limiter *ratelimit.Limiter
	buckets core.Buckets
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	henrik, official := p.henrikSources(cfg.APIKey), p.officialSources()

	for _, e := range []endpointSpec{
		{name: "rank", ttl: rankTTL, id: channelID, fallback: "rank lookup failed",
			reply: func(id, msg string) any { return rankReply{Player: id, Error: msg} },
			src:   firstSource("rank", henrik.rank, official.rank)},
		{name: "record", ttl: recordTTL, id: channelID, fallback: "record lookup failed",
			reply: func(id, msg string) any { return recordReply{Player: id, Error: msg} },
			src:   firstSource("record", henrik.record)},
		{name: "matches", ttl: matchesTTL, id: channelID, fallback: "match history lookup failed",
			reply: func(id, msg string) any { return matchesReply{Player: id, Error: msg} },
			src:   firstSource("matches", official.matches, henrik.matches)},
		{name: "account", ttl: accountTTL, id: accountID, fallback: "account lookup failed",
			reply: func(id, msg string) any { return accountReply{Player: id, Error: msg} },
			src:   firstSource("account", official.account, henrik.account)},
		{name: "leaderboard", ttl: boardTTL, id: boardID, fallback: "leaderboard lookup failed",
			reply: func(id, msg string) any { return leaderboardReply{Player: id, Error: msg} },
			src:   firstSource("leaderboard", official.board, henrik.board)},
	} {
		b.Endpoint(e.name).Timeout(handlerTimeout).
			Cached(e.ttl, negativeTTL).
			ID(e.id).
			Reply(e.reply).
			Budget(e.src.budget).
			Fallback(e.fallback).
			Fetch(e.src.fetch)
	}

	b.Endpoint("link").Timeout(handlerTimeout).Handle(p.linkExchange)

	return b.Build()
}

type endpointSpec struct {
	name     string
	ttl      time.Duration
	id       provider.IDFunc
	reply    provider.ReplyFunc
	fallback string
	src      source
}

type source struct {
	fetch  provider.FetchFunc
	budget provider.AdmitFunc
}

type sources struct {
	rank, record, matches, account, board *source
}

func (p *api) henrikSources(key string) sources {
	if key == "" {
		return sources{}
	}
	via := func(fetch provider.FetchFunc) *source { return &source{fetch: fetch, budget: p.budget} }
	return sources{
		rank:    via(p.rankFetch),
		record:  via(p.recordFetch),
		matches: via(p.matchesFetch),
		account: via(p.accountFetch),
		board:   via(p.boardFetch),
	}
}

func firstSource(endpoint string, candidates ...*source) source {
	for _, c := range candidates {
		if c != nil {
			return *c
		}
	}
	return source{
		fetch: func(context.Context, gossiprpc.Request, provider.ID) (any, error) {
			return nil, &core.UpstreamError{Status: 404, Message: "valorant " + endpoint + " is not available right now"}
		},
		budget: func(context.Context, gossiprpc.Request) error { return nil },
	}
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
	return &api{
		// HenrikDev takes the raw key; a "Bearer" prefix gets 401 on every call.
		http: b.Client(base, map[string]string{
			"Authorization": cfg.APIKey,
		}, httpTimeout),
		content: b.Client(content, nil, httpTimeout),
		riot:    newRiot(cfg.Riot, b),
		links:   d.RiotLinks,
		log:     d.Logger(),
		cache:   d.Cache,
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:valorant", cfg.RateLimit, rateWindowSeconds),
	}
}

func (p *api) budget(ctx context.Context, req gossiprpc.Request) error {
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

type riotIDValue struct {
	name string
	tag  string
}

func (r riotIDValue) String() string { return r.name + "#" + r.tag }

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

func scope(req gossiprpc.Request) (region, platform, reject string) {
	region, reject = normalizeRegion(req.Region)
	if reject != "" {
		return "", "", reject
	}
	platform, reject = normalizePlatform(req.Platform)
	return region, platform, reject
}

func channelID(req gossiprpc.Request) (provider.ID, string) {
	channel := strings.TrimSpace(req.ChannelID)
	if channel == "" {
		return provider.ID{}, "missing channel"
	}
	region, platform, reject := scope(req)
	if reject != "" {
		return provider.ID{}, reject
	}
	return provider.ID{Key: core.CacheID(channel, region, platform)}, ""
}

func accountID(req gossiprpc.Request) (provider.ID, string) {
	id, reject := channelID(req)
	id.Key = core.CacheID(strings.TrimSpace(req.ChannelID))
	return id, reject
}

func boardID(req gossiprpc.Request) (provider.ID, string) {
	channel := strings.TrimSpace(req.ChannelID)
	region, platform, reject := scope(req)
	if reject != "" {
		return provider.ID{}, reject
	}
	if channel == "" && region == "auto" {
		return provider.ID{}, "missing region (no linked account to detect it from)"
	}
	return provider.ID{Key: core.CacheID(channel, region, platform)}, ""
}

const notLinkedMessage = "link a Riot account on the ItsBagelBot dashboard first"

type linkedPlayer struct {
	id    riotIDValue
	puuid string
	shard string
}

type linkWire struct {
	Name  string `json:"name"`
	Tag   string `json:"tag"`
	Puuid string `json:"puuid"`
	Shard string `json:"shard"`
}

func (p *api) linked(ctx context.Context, channel string) (linkedPlayer, error) {
	key := core.Key(providerName, "link", channel)
	wire, err := core.Cached(ctx, p.cache, key, linkTTL, linkTTL, nil, func(ctx context.Context) (linkWire, error) {
		if p.links == nil {
			return linkWire{}, &core.UpstreamError{Status: 404, Message: "Riot accounts can't be read right now"}
		}
		link, err := p.links.RiotLink(ctx, channel)
		if err != nil {
			return linkWire{}, err
		}
		if !link.Linked {
			return linkWire{}, &core.UpstreamError{Status: 404, Message: notLinkedMessage}
		}
		return linkWire{Name: link.GameName, Tag: link.TagLine, Puuid: link.Puuid, Shard: link.Shard}, nil
	})
	if err != nil {
		return linkedPlayer{}, err
	}
	return linkedPlayer{id: riotIDValue{name: wire.Name, tag: wire.Tag}, puuid: wire.Puuid, shard: wire.Shard}, nil
}

type lookup struct {
	player   linkedPlayer
	region   string
	platform string
}

func (p *api) lookup(ctx context.Context, req gossiprpc.Request) (lookup, error) {
	player, err := p.linked(ctx, strings.TrimSpace(req.ChannelID))
	if err != nil {
		return lookup{}, err
	}
	region, platform, _ := scope(req)
	if region == "auto" {
		region = player.shard
	}
	return lookup{player: player, region: region, platform: platform}, nil
}

func (l lookup) henrikPath(prefix string) string {
	return prefix + l.region + "/" + l.platform + "/" + url.PathEscape(l.player.id.name) + "/" + url.PathEscape(l.player.id.tag)
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

func (p *api) fetchAccount(ctx context.Context, id riotIDValue) (accountInfo, error) {
	var wire accountWire
	path := "/valorant/v2/account/" + url.PathEscape(id.name) + "/" + url.PathEscape(id.tag)
	if err := p.http.GetJSON(ctx, path, nil, &wire); err != nil {
		return accountInfo{}, err
	}
	if strings.TrimSpace(wire.Data.Puuid) == "" {
		return accountInfo{}, &core.UpstreamError{Status: 404, Message: "player not found"}
	}
	return wire.Data, nil
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

func (p *api) fetchMMR(ctx context.Context, req gossiprpc.Request, wire any) (lookup, error) {
	at, err := p.lookup(ctx, req)
	if err != nil {
		return lookup{}, err
	}
	if err := p.http.GetJSON(ctx, at.henrikPath("/valorant/v3/mmr/"), nil, wire); err != nil {
		return lookup{}, err
	}
	return at, nil
}

func (p *api) rankFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	var wire mmrWire
	lookup, err := p.fetchMMR(ctx, req, &wire)
	if err != nil {
		return nil, err
	}
	mmr := wire.Data
	reply := rankReply{
		Player:     lookup.player.id.String(),
		Region:     lookup.region,
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

// Decoded apart from mmrWire so an upstream shape change in seasonal cannot fail rank lookups.
type recordWire struct {
	Data struct {
		Seasonal []seasonRecord `json:"seasonal"`
	} `json:"data"`
}

type seasonRecord struct {
	Games int `json:"games"`
	Wins  int `json:"wins"`
}

type recordReply struct {
	Player string `json:"player"`
	Region string `json:"region"`
	Games  int    `json:"games"`
	Wins   int    `json:"wins"`
	Empty  bool   `json:"empty"`
	Error  string `json:"error,omitempty"`
}

func (p *api) recordFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	var wire recordWire
	lookup, err := p.fetchMMR(ctx, req, &wire)
	if err != nil {
		return nil, err
	}
	reply := recordReply{Player: lookup.player.id.String(), Region: lookup.region}
	for _, season := range wire.Data.Seasonal {
		reply.Games += season.Games
		reply.Wins += season.Wins
	}
	reply.Empty = reply.Games == 0
	return reply, nil
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
	at, err := p.lookup(ctx, req)
	if err != nil {
		return nil, err
	}
	var history matchesWire
	query := url.Values{"mode": {"competitive"}, "size": {strconv.Itoa(matchCount)}}
	if err := p.http.GetJSON(ctx, at.henrikPath("/valorant/v4/matches/"), query, &history); err != nil {
		return nil, err
	}
	entries := summarizeMatches(history.Data, byRiotID(at.player.id), time.Now())
	return matchesReply{
		Player:  at.player.id.String(),
		Region:  at.region,
		Matches: entries,
		Empty:   len(entries) == 0,
	}, nil
}

type selfMatcher func(matchPlayer) bool

func byRiotID(rid riotIDValue) selfMatcher {
	return func(player matchPlayer) bool {
		return strings.EqualFold(player.Name, rid.name) && strings.EqualFold(player.Tag, rid.tag)
	}
}

func byPuuid(puuid string) selfMatcher {
	return func(player matchPlayer) bool { return player.Puuid == puuid }
}

func summarizeMatches(matches []matchV4, isSelf selfMatcher, now time.Time) []matchEntry {
	entries := make([]matchEntry, 0, len(matches))
	for _, match := range matches {
		if !match.Metadata.IsCompleted {
			continue
		}
		player, ok := findSelf(match.Players, isSelf)
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
	return entries
}

func findSelf(players []matchPlayer, isSelf selfMatcher) (matchPlayer, bool) {
	for _, player := range players {
		if isSelf(player) {
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
	Puuid        string `json:"puuid,omitempty"`
	Region       string `json:"region,omitempty"`
	AccountLevel int    `json:"account_level,omitempty"`
	Card         string `json:"card,omitempty"`
	Title        string `json:"title,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (p *api) accountFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	at, err := p.lookup(ctx, req)
	if err != nil {
		return nil, err
	}
	info, err := p.fetchAccount(ctx, at.player.id)
	if err != nil {
		return nil, err
	}
	return accountReplyFor(at.player.id, info), nil
}

func accountReplyFor(rid riotIDValue, info accountInfo) accountReply {
	return accountReply{
		Player:       rid.String(),
		Puuid:        info.Puuid,
		Region:       strings.ToLower(info.Region),
		AccountLevel: info.AccountLevel,
		Card:         info.Card,
		Title:        info.Title,
	}
}

type boardWire struct {
	Data struct {
		Players []boardPlayer `json:"players"`
	} `json:"data"`
}

type boardPlayer struct {
	LeaderboardRank int    `json:"leaderboard_rank"`
	Anonymized      bool   `json:"is_anonymized"`
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
	region, platform, err := p.boardScope(ctx, req)
	if err != nil {
		return nil, err
	}
	var board boardWire
	if err := p.http.GetJSON(ctx, "/valorant/v3/leaderboard/"+region+"/"+platform, nil, &board); err != nil {
		return nil, err
	}
	return boardReplyFor(board.Data.Players, id.Display, region+"/"+platform), nil
}

func (p *api) boardScope(ctx context.Context, req gossiprpc.Request) (region, platform string, err error) {
	region, platform, _ = scope(req)
	if region != "auto" {
		return region, platform, nil
	}
	at, err := p.lookup(ctx, req)
	return at.region, platform, err
}

func boardReplyFor(players []boardPlayer, display, board string) leaderboardReply {
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
			Player: boardName(player),
			Tier:   player.Tier,
			RR:     player.RR,
			Wins:   player.Wins,
		})
	}
	return leaderboardReply{
		Player:  display,
		Board:   board,
		Entries: entries,
		Empty:   len(entries) == 0,
	}
}

func boardName(player boardPlayer) string {
	if player.Anonymized || player.Name == "" {
		return "Secret Agent"
	}
	return player.Name + "#" + player.Tag
}
