// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package clashroyale

import (
	"context"
	"math"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"ItsBagelBot/pkg/ratelimit"
)

const (
	defaultBaseURL = "https://proxy.royaleapi.dev/v1"

	profileTTL  = 5 * time.Minute
	negativeTTL = 5 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	rateWindowSeconds = 60.0
)

type Config struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
}

const providerName = "clashroyale"

type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	limiter *ratelimit.Limiter
	buckets core.Buckets
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	p.view(b, "stats", func(tag, msg string) any { return gossiprpc.ClashRoyaleStatsReply{Tag: tag, Error: msg} }, shapeStats)
	p.view(b, "decks", func(tag, msg string) any { return gossiprpc.ClashRoyaleDecksReply{Tag: tag, Error: msg} }, shapeDecks)
	p.view(b, "ranked", func(tag, msg string) any { return gossiprpc.ClashRoyaleRankedReply{Tag: tag, Error: msg} }, shapeRanked)
	p.view(b, "trophy_road", func(tag, msg string) any { return gossiprpc.ClashRoyaleTrophyRoadReply{Tag: tag, Error: msg} }, shapeTrophyRoad)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 600
	}
	return &api{
		http: b.Client(base, map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
		}, httpTimeout),
		cache:   d.Cache,
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:clashroyale", cfg.RateLimit, rateWindowSeconds),
	}
}

func (p *api) view(b *provider.Builder, name string, errReply provider.ReplyFunc, shape func(playerProfile) any) {
	b.Endpoint(name).Timeout(handlerTimeout).
		Cached(profileTTL, negativeTTL).
		ID(tagID).
		Reply(errReply).
		Budget(p.profileBudget).
		Fallback(name + " lookup failed").
		Fetch(p.profileFetch(shape))
}

func tagID(req gossiprpc.Request) (provider.ID, string) {
	tag, msg := parsePlayerTag(req.Account)
	if msg != "" {
		return provider.ID{Display: strings.TrimSpace(req.Account)}, msg
	}
	return provider.ID{Display: tag.String(), Key: tag.cacheKey()}, ""
}

func (p *api) profileBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

func (p *api) profileFetch(shape func(playerProfile) any) provider.FetchFunc {
	return func(ctx context.Context, _ gossiprpc.Request, id provider.ID) (any, error) {
		tag, _ := parsePlayerTag(id.Display)
		profile, err := p.profile(ctx, tag)
		if err != nil {
			return nil, err
		}
		return shape(profile), nil
	}
}

type playerTag string

const tagAlphabet = "0289PYLQGRJCUV"

var tagRuneTable = buildTagRuneTable()

func buildTagRuneTable() [128]bool {
	var table [128]bool
	for _, r := range tagAlphabet {
		table[r] = true
	}
	return table
}

func isTagRune(r rune) bool {
	return uint32(r) < uint32(len(tagRuneTable)) && tagRuneTable[r]
}

func parsePlayerTag(account string) (playerTag, string) {
	tag := strings.ToUpper(strings.TrimSpace(account))
	if tag == "" {
		return "", "missing account"
	}
	tag = strings.TrimPrefix(tag, "#")
	tag = strings.ReplaceAll(tag, "O", "0")
	if len(tag) < 3 || len(tag) > 15 {
		return "", "invalid player tag"
	}
	for _, r := range tag {
		if !isTagRune(r) {
			return "", "invalid player tag"
		}
	}
	return playerTag(tag), ""
}

func (t playerTag) String() string   { return "#" + string(t) }
func (t playerTag) cacheKey() string { return strings.ToLower(string(t)) }

type playerProfile struct {
	Tag                       string                            `json:"tag"`
	Name                      string                            `json:"name"`
	ExpLevel                  int                               `json:"expLevel"`
	ExpPoints                 int64                             `json:"expPoints"`
	StarPoints                int64                             `json:"starPoints"`
	Trophies                  int                               `json:"trophies"`
	BestTrophies              int                               `json:"bestTrophies"`
	Wins                      int                               `json:"wins"`
	Losses                    int                               `json:"losses"`
	BattleCount               int                               `json:"battleCount"`
	ThreeCrownWins            int                               `json:"threeCrownWins"`
	ChallengeCardsWon         int                               `json:"challengeCardsWon"`
	ChallengeMaxWins          int                               `json:"challengeMaxWins"`
	TournamentCardsWon        int                               `json:"tournamentCardsWon"`
	TournamentBattleCount     int                               `json:"tournamentBattleCount"`
	Donations                 int                               `json:"donations"`
	DonationsReceived         int                               `json:"donationsReceived"`
	TotalDonations            int                               `json:"totalDonations"`
	Arena                     gossiprpc.ClashRoyaleArena        `json:"arena"`
	Clan                      gossiprpc.ClashRoyaleClan         `json:"clan"`
	CurrentFavouriteCard      gossiprpc.ClashRoyaleCard         `json:"currentFavouriteCard"`
	CurrentDeck               []gossiprpc.ClashRoyaleCard       `json:"currentDeck"`
	CurrentDeckSupportCards   []gossiprpc.ClashRoyaleCard       `json:"currentDeckSupportCards"`
	LeagueStatistics          leagueStats                       `json:"leagueStatistics"`
	CurrentPathOfLegendResult gossiprpc.ClashRoyaleRankedResult `json:"currentPathOfLegendSeasonResult"`
	LastPathOfLegendResult    gossiprpc.ClashRoyaleRankedResult `json:"lastPathOfLegendSeasonResult"`
	BestPathOfLegendResult    gossiprpc.ClashRoyaleRankedResult `json:"bestPathOfLegendSeasonResult"`
}

type leagueStats struct {
	Current  gossiprpc.ClashRoyaleRankedResult `json:"currentSeason"`
	Previous gossiprpc.ClashRoyaleRankedResult `json:"previousSeason"`
	Best     gossiprpc.ClashRoyaleRankedResult `json:"bestSeason"`
}

func (p *api) profile(ctx context.Context, tag playerTag) (playerProfile, error) {
	key := core.Key(providerName, "profile", tag.cacheKey())
	return core.Cached(ctx, p.cache, key, profileTTL, negativeTTL, nil, func(ctx context.Context) (playerProfile, error) {
		var profile playerProfile
		path := "/players/" + url.PathEscape(tag.String())
		if err := p.http.GetJSON(ctx, path, nil, &profile); err != nil {
			return playerProfile{}, err
		}
		if strings.TrimSpace(profile.Tag) == "" {
			return playerProfile{}, &core.UpstreamError{Status: 404, Message: "player not found"}
		}
		return profile, nil
	})
}

func shapeStats(profile playerProfile) any {
	draws := profile.BattleCount - profile.Wins - profile.Losses
	if draws < 0 {
		draws = 0
	}
	winRate := 0.0
	if profile.BattleCount > 0 {
		winRate = float64(profile.Wins) * 100 / float64(profile.BattleCount)
	}
	return gossiprpc.ClashRoyaleStatsReply{
		Player: profile.Name, Tag: profile.Tag, KingLevel: profile.ExpLevel,
		ExperiencePoints: profile.ExpPoints, StarPoints: profile.StarPoints,
		Wins: profile.Wins, Losses: profile.Losses, Draws: draws,
		Battles: profile.BattleCount, WinRate: winRate,
		ThreeCrownWins:    profile.ThreeCrownWins,
		ChallengeCardsWon: profile.ChallengeCardsWon, ChallengeMaxWins: profile.ChallengeMaxWins,
		TournamentCardsWon: profile.TournamentCardsWon, TournamentBattleCount: profile.TournamentBattleCount,
		Donations: profile.Donations, DonationsReceived: profile.DonationsReceived,
		TotalDonations: profile.TotalDonations, Clan: profile.Clan,
		FavouriteCard: profile.CurrentFavouriteCard,
	}
}

func shapeDecks(profile playerProfile) any {
	var total int
	for _, c := range profile.CurrentDeck {
		total += c.ElixirCost
	}
	average := 0.0
	if len(profile.CurrentDeck) > 0 {
		average = math.Round((float64(total)/float64(len(profile.CurrentDeck)))*100) / 100
	}
	return gossiprpc.ClashRoyaleDecksReply{
		Player: profile.Name, Tag: profile.Tag, CurrentDeck: profile.CurrentDeck,
		SupportCards: profile.CurrentDeckSupportCards, AverageElixir: average,
	}
}

func hasRankedResult(r gossiprpc.ClashRoyaleRankedResult) bool {
	return r.SeasonID != "" || r.LeagueNumber != 0 || r.Trophies != 0 || r.BestTrophies != 0 || r.Rank != 0
}

func preferRanked(primary, fallback gossiprpc.ClashRoyaleRankedResult) gossiprpc.ClashRoyaleRankedResult {
	if hasRankedResult(primary) {
		return primary
	}
	return fallback
}

func shapeRanked(profile playerProfile) any {
	current := preferRanked(profile.CurrentPathOfLegendResult, profile.LeagueStatistics.Current)
	previous := preferRanked(profile.LastPathOfLegendResult, profile.LeagueStatistics.Previous)
	best := preferRanked(profile.BestPathOfLegendResult, profile.LeagueStatistics.Best)
	return gossiprpc.ClashRoyaleRankedReply{
		Player: profile.Name, Tag: profile.Tag, Current: current, Previous: previous, Best: best,
		Unranked: !hasRankedResult(current),
	}
}

func shapeTrophyRoad(profile playerProfile) any {
	return gossiprpc.ClashRoyaleTrophyRoadReply{
		Player: profile.Name, Tag: profile.Tag, Trophies: profile.Trophies,
		BestTrophies: profile.BestTrophies, Arena: profile.Arena,
	}
}
