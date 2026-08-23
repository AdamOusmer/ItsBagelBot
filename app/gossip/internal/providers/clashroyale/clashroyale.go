// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package clashroyale exposes the official Supercell Clash Royale player API
// through RoyaleAPI's supported proxy. The stats, decks, ranked, and
// trophy_road endpoints all derive from GET /players/{playerTag}; a shared
// profile cache means reading several views still costs one upstream request.
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

	// RateLimit is configured as requests per minute.
	rateWindowSeconds = 60.0
)

// Config carries the official API host, bearer token, and per-minute request
// budget. APIKey must be non-empty; providers.All skips this provider otherwise.
type Config struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
}

// providerName is the subject token this provider answers under.
const providerName = "clashroyale"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	limiter *ratelimit.Limiter
	buckets core.Buckets
}

// New builds a Clash Royale provider: four byte-flow views over one shared
// profile cache, so reading several views still costs one upstream request.
func New(cfg Config, d provider.Deps) provider.Provider {
	p := newAPI(cfg, d)
	b := provider.NewProvider(providerName, d)
	p.view(b, "stats", func(tag, msg string) any { return gossiprpc.ClashRoyaleStatsReply{Tag: tag, Error: msg} }, shapeStats)
	p.view(b, "decks", func(tag, msg string) any { return gossiprpc.ClashRoyaleDecksReply{Tag: tag, Error: msg} }, shapeDecks)
	p.view(b, "ranked", func(tag, msg string) any { return gossiprpc.ClashRoyaleRankedReply{Tag: tag, Error: msg} }, shapeRanked)
	p.view(b, "trophy_road", func(tag, msg string) any { return gossiprpc.ClashRoyaleTrophyRoadReply{Tag: tag, Error: msg} }, shapeTrophyRoad)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 600
	}
	return &api{
		http: core.NewHTTPClient(base, map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
		}, httpTimeout),
		cache:   d.Cache,
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:clashroyale", cfg.RateLimit, rateWindowSeconds),
	}
}

// view declares one byte-flow endpoint that projects the shared player profile
// through shape.
func (p *api) view(b *provider.Builder, name string, errReply provider.ReplyFunc, shape func(playerProfile) any) {
	b.Endpoint(name).Timeout(handlerTimeout).
		Cached(profileTTL, negativeTTL).
		ID(tagID).
		Reply(errReply).
		Budget(p.profileBudget).
		Fallback(name + " lookup failed").
		Fetch(p.profileFetch(shape))
}

// tagID validates and canonicalizes the player tag: the reply echoes the
// canonical "#TAG" form once it parses, or the raw input on a validation
// error.
func tagID(req gossiprpc.Request) (provider.ID, string) {
	tag, msg := parsePlayerTag(req.Account)
	if msg != "" {
		return provider.ID{Display: strings.TrimSpace(req.Account)}, msg
	}
	return provider.ID{Display: tag.String(), Key: tag.cacheKey()}, ""
}

// profileFetch loads the shared profile and projects it through shape.
// profileBudget spends one request's share of the Clash Royale allowance in that
// request's own lane. Every view shares one profile entry, so it is declared on
// each endpoint rather than written inside the shared fill: a check in there runs
// once per singleflight flight, is charged to whichever caller won it, and hands
// that verdict to everyone joined — which let a drained standard bucket deny
// premium callers the reserve they are entitled to.
func (p *api) profileBudget(ctx context.Context, req gossiprpc.Request) error {
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

func (p *api) profileFetch(shape func(playerProfile) any) provider.FetchFunc {
	return func(ctx context.Context, _ gossiprpc.Request, id provider.ID) (any, error) {
		tag, _ := parsePlayerTag(id.Display) // already validated by tagID
		profile, err := p.profile(ctx, tag)
		if err != nil {
			return nil, err
		}
		return shape(profile), nil
	}
}

// playerTag is the canonical tag without its leading hash. Clash Royale tags
// use a deliberately restricted alphabet so visually ambiguous characters do
// not occur.
type playerTag string

const tagAlphabet = "0289PYLQGRJCUV"

func parsePlayerTag(account string) (playerTag, string) {
	tag := strings.ToUpper(strings.TrimSpace(account))
	if tag == "" {
		return "", "missing account"
	}
	tag = strings.TrimPrefix(tag, "#")
	// O is not part of Supercell's tag alphabet, but it is the most common
	// transcription of zero. RoyaleAPI recommends normalizing it for users.
	tag = strings.ReplaceAll(tag, "O", "0")
	if len(tag) < 3 || len(tag) > 15 {
		return "", "invalid player tag"
	}
	for _, r := range tag {
		if !strings.ContainsRune(tagAlphabet, r) {
			return "", "invalid player tag"
		}
	}
	return playerTag(tag), ""
}

func (t playerTag) String() string   { return "#" + string(t) }
func (t playerTag) cacheKey() string { return strings.ToLower(string(t)) }

// playerProfile is the current official player payload subset used by all
// four views. Unknown upstream additions are ignored by encoding/json. Nested
// values reuse the shared reply shapes: their JSON keys mirror the upstream's
// own, so the profile decodes straight into them and shaping is projection
// only.
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
