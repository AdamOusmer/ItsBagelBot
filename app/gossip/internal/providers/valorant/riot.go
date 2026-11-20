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
)

const (
	defaultRiotBaseURL = "https://{route}.api.riotgames.com"
	defaultRiotAuthURL = "https://auth.riotgames.com"
	riotAccountRoute   = "americas"

	matchDetailTTL = 24 * time.Hour
	assetTTL       = 24 * time.Hour
	actTTL         = time.Hour

	competitiveQueue = "competitive"
)

type RiotConfig struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
	SignOn    SignOnConfig
}

type SignOnConfig struct {
	AuthBaseURL  string
	ClientID     string
	ClientSecret string
}

type riotAPI struct {
	account *core.HTTPClient
	auth    *core.HTTPClient
	signOn  SignOnConfig
	shards  map[string]*core.HTTPClient
	buckets core.Buckets
}

func newRiot(cfg RiotConfig, b *provider.Builder) *riotAPI {
	if cfg.APIKey == "" {
		return nil
	}
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = defaultRiotBaseURL
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 300
	}
	headers := map[string]string{"X-Riot-Token": cfg.APIKey}
	client := func(route string) *core.HTTPClient {
		return b.Client(strings.ReplaceAll(base, "{route}", route), headers, httpTimeout)
	}
	r := &riotAPI{
		account: client(riotAccountRoute),
		auth:    b.Client(orDefaultURL(cfg.SignOn.AuthBaseURL, defaultRiotAuthURL), nil, httpTimeout),
		signOn:  cfg.SignOn,
		shards:  make(map[string]*core.HTTPClient, len(affinities)),
		buckets: core.NewBuckets("ratelimit:gossip:valorant:riot", cfg.RateLimit, rateWindowSeconds),
	}
	for shard := range affinities {
		r.shards[shard] = client(shard)
	}
	return r
}

func (p *api) officialSources() sources {
	if p.riot == nil {
		return sources{}
	}
	via := func(fetch provider.FetchFunc) *source { return &source{fetch: fetch, budget: p.riotBudget} }
	return sources{
		rank:    via(p.riotRankFetch),
		matches: via(p.riotMatchesFetch),
		account: via(p.riotAccountFetch),
		board:   via(p.riotBoardFetch),
	}
}

func (p *api) riotBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.riot.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

type riotAt struct {
	lookup
	client *core.HTTPClient
}

func (p *api) riotLocate(ctx context.Context, req gossiprpc.Request) (riotAt, error) {
	at, err := p.lookup(ctx, req)
	if err != nil {
		return riotAt{}, err
	}
	client, ok := p.riot.shards[at.region]
	if !ok {
		return riotAt{}, &core.UpstreamError{Status: 404, Message: "could not detect region"}
	}
	return riotAt{lookup: at, client: client}, nil
}

func (p *api) riotAccountFetch(ctx context.Context, req gossiprpc.Request, _ provider.ID) (any, error) {
	at, err := p.riotLocate(ctx, req)
	if err != nil {
		return nil, err
	}
	return accountReply{Player: at.player.id.String(), Puuid: at.player.puuid, Region: at.region}, nil
}

type matchlistWire struct {
	History []struct {
		MatchID   string `json:"matchId"`
		StartedMs int64  `json:"gameStartTimeMillis"`
		QueueID   string `json:"queueId"`
	} `json:"history"`
}

type riotMatch struct {
	MatchInfo struct {
		MapID       string `json:"mapId"`
		StartedMs   int64  `json:"gameStartMillis"`
		IsCompleted bool   `json:"isCompleted"`
	} `json:"matchInfo"`
	Players []riotPlayer `json:"players"`
	Teams   []struct {
		TeamID       string `json:"teamId"`
		Won          bool   `json:"won"`
		RoundsPlayed int    `json:"roundsPlayed"`
		RoundsWon    int    `json:"roundsWon"`
	} `json:"teams"`
}

type riotPlayer struct {
	Puuid           string `json:"puuid"`
	GameName        string `json:"gameName"`
	TagLine         string `json:"tagLine"`
	TeamID          string `json:"teamId"`
	CharacterID     string `json:"characterId"`
	CompetitiveTier int    `json:"competitiveTier"`
	Stats           struct {
		Score   int `json:"score"`
		Kills   int `json:"kills"`
		Deaths  int `json:"deaths"`
		Assists int `json:"assists"`
	} `json:"stats"`
}

func (p *api) riotRecent(ctx context.Context, at riotAt, count int) ([]riotMatch, error) {
	var list matchlistWire
	if err := at.client.GetJSON(ctx, "/val/match/v1/matchlists/by-puuid/"+url.PathEscape(at.player.puuid), nil, &list); err != nil {
		return nil, err
	}
	history := list.History
	sort.SliceStable(history, func(i, j int) bool { return history[i].StartedMs > history[j].StartedMs })
	matches := make([]riotMatch, 0, count)
	for _, entry := range history {
		if len(matches) == count {
			break
		}
		if entry.QueueID != competitiveQueue {
			continue
		}
		match, err := p.riotMatchDetail(ctx, at, entry.MatchID)
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, nil
}

func (p *api) riotMatchDetail(ctx context.Context, at riotAt, matchID string) (riotMatch, error) {
	key := core.Key(providerName, "riot-match", at.region+"/"+matchID)
	return core.Cached(ctx, p.cache, key, matchDetailTTL, negativeTTL, nil, func(ctx context.Context) (riotMatch, error) {
		var match riotMatch
		err := at.client.GetJSON(ctx, "/val/match/v1/matches/"+url.PathEscape(matchID), nil, &match)
		return match, err
	})
}

func (p *api) riotMatchesFetch(ctx context.Context, req gossiprpc.Request, _ provider.ID) (any, error) {
	at, err := p.riotLocate(ctx, req)
	if err != nil {
		return nil, err
	}
	matches, err := p.riotRecent(ctx, at, matchCount)
	if err != nil {
		return nil, err
	}
	maps, err := p.assetNames(ctx, mapAssets)
	if err != nil {
		return nil, err
	}
	agents, err := p.assetNames(ctx, agentAssets)
	if err != nil {
		return nil, err
	}
	converted := make([]matchV4, 0, len(matches))
	for _, match := range matches {
		converted = append(converted, match.asV4(maps, agents))
	}
	entries := summarizeMatches(converted, byPuuid(at.player.puuid), time.Now())
	return matchesReply{Player: at.player.id.String(), Region: at.region, Matches: entries, Empty: len(entries) == 0}, nil
}

func (m riotMatch) asV4(maps, agents map[string]string) matchV4 {
	out := matchV4{Metadata: matchMeta{
		Map:         mapCombo{ID: m.MatchInfo.MapID, Name: maps[m.MatchInfo.MapID]},
		StartedAt:   time.UnixMilli(m.MatchInfo.StartedMs),
		IsCompleted: m.MatchInfo.IsCompleted,
	}}
	for _, player := range m.Players {
		out.Players = append(out.Players, matchPlayer{
			Puuid: player.Puuid, Name: player.GameName, Tag: player.TagLine, TeamID: player.TeamID,
			Agent: agentCombo{Name: agents[strings.ToLower(player.CharacterID)]},
			Statistics: matchStats{
				Kills: player.Stats.Kills, Deaths: player.Stats.Deaths,
				Assists: player.Stats.Assists, Score: player.Stats.Score,
			},
		})
	}
	for _, team := range m.Teams {
		out.Teams = append(out.Teams, matchTeam{
			TeamID: team.TeamID, Won: team.Won,
			Rounds: teamRounds{Won: team.RoundsWon, Lost: team.RoundsPlayed - team.RoundsWon},
		})
	}
	return out
}

func (m riotMatch) tierOf(puuid string) int {
	for _, player := range m.Players {
		if player.Puuid == puuid {
			return player.CompetitiveTier
		}
	}
	return 0
}

func (p *api) riotRankFetch(ctx context.Context, req gossiprpc.Request, _ provider.ID) (any, error) {
	at, err := p.riotLocate(ctx, req)
	if err != nil {
		return nil, err
	}
	latest, err := p.riotRecent(ctx, at, 1)
	if err != nil {
		return nil, err
	}
	reply := rankReply{Player: at.player.id.String(), Region: at.region, Tier: "Unranked", Unranked: true}
	if len(latest) == 0 {
		return reply, nil
	}
	tier := latest[0].tierOf(at.player.puuid)
	if tier == 0 {
		return reply, nil
	}
	tiers, err := p.assetNames(ctx, tierAssets)
	if err != nil {
		return nil, err
	}
	reply.Tier, reply.Unranked = tiers[strconv.Itoa(tier)], false
	return reply, nil
}

type contentsWire struct {
	Acts []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		IsActive bool   `json:"isActive"`
	} `json:"acts"`
}

func (p *api) activeAct(ctx context.Context, shard string) (string, error) {
	key := core.Key(providerName, "riot-act", shard)
	return core.Cached(ctx, p.cache, key, actTTL, negativeTTL, nil, func(ctx context.Context) (string, error) {
		var contents contentsWire
		if err := p.riot.shards[shard].GetJSON(ctx, "/val/content/v1/contents", nil, &contents); err != nil {
			return "", err
		}
		for _, act := range contents.Acts {
			if act.IsActive && strings.EqualFold(act.Type, "act") {
				return act.ID, nil
			}
		}
		return "", &core.UpstreamError{Status: 404, Message: "no active act"}
	})
}

type riotBoardWire struct {
	Players []struct {
		LeaderboardRank int    `json:"leaderboardRank"`
		GameName        string `json:"gameName"`
		TagLine         string `json:"tagLine"`
		RankedRating    int    `json:"rankedRating"`
		NumberOfWins    int    `json:"numberOfWins"`
		CompetitiveTier int    `json:"competitiveTier"`
	} `json:"players"`
}

func (p *api) riotBoardFetch(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
	shard, platform, err := p.boardScope(ctx, req)
	if err != nil {
		return nil, err
	}
	if platform != "pc" {
		return nil, &core.UpstreamError{Status: 404, Message: "console leaderboards are not available right now"}
	}
	if _, ok := p.riot.shards[shard]; !ok {
		return nil, &core.UpstreamError{Status: 404, Message: "could not detect region"}
	}
	act, err := p.activeAct(ctx, shard)
	if err != nil {
		return nil, err
	}
	var board riotBoardWire
	query := url.Values{"size": {strconv.Itoa(boardEntries)}, "startIndex": {"0"}}
	if err := p.riot.shards[shard].GetJSON(ctx, "/val/ranked/v1/leaderboards/by-act/"+url.PathEscape(act), query, &board); err != nil {
		return nil, err
	}
	players := make([]boardPlayer, 0, len(board.Players))
	for _, player := range board.Players {
		players = append(players, boardPlayer{
			LeaderboardRank: player.LeaderboardRank, Name: player.GameName, Tag: player.TagLine,
			Wins: player.NumberOfWins, RR: player.RankedRating, Tier: player.CompetitiveTier,
		})
	}
	return boardReplyFor(players, id.Display, shard+"/"+platform), nil
}

type assetKind struct {
	name  string
	path  string
	query url.Values
	index func(assetsWire) map[string]string
}

type assetsWire struct {
	Data []struct {
		UUID        string `json:"uuid"`
		DisplayName string `json:"displayName"`
		MapURL      string `json:"mapUrl"`
		Tiers       []struct {
			Tier     int    `json:"tier"`
			TierName string `json:"tierName"`
		} `json:"tiers"`
	} `json:"data"`
}

var (
	mapAssets = assetKind{name: "maps", path: "/v1/maps", index: func(w assetsWire) map[string]string {
		out := make(map[string]string, len(w.Data))
		for _, m := range w.Data {
			out[m.MapURL] = m.DisplayName
		}
		return out
	}}
	agentAssets = assetKind{name: "agents", path: "/v1/agents", query: url.Values{"isPlayableCharacter": {"true"}},
		index: func(w assetsWire) map[string]string {
			out := make(map[string]string, len(w.Data))
			for _, a := range w.Data {
				out[strings.ToLower(a.UUID)] = a.DisplayName
			}
			return out
		}}
	tierAssets = assetKind{name: "competitivetiers", path: "/v1/competitivetiers", index: func(w assetsWire) map[string]string {
		out := map[string]string{}
		if len(w.Data) == 0 {
			return out
		}
		for _, t := range w.Data[len(w.Data)-1].Tiers {
			out[strconv.Itoa(t.Tier)] = titleCase(t.TierName)
		}
		return out
	}}
)

func (p *api) assetNames(ctx context.Context, kind assetKind) (map[string]string, error) {
	key := core.Key(providerName, "assets", kind.name)
	return core.Cached(ctx, p.cache, key, assetTTL, negativeTTL, nil, func(ctx context.Context) (map[string]string, error) {
		var wire assetsWire
		if err := p.content.GetJSON(ctx, kind.path, kind.query, &wire); err != nil {
			return nil, err
		}
		return kind.index(wire), nil
	})
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
