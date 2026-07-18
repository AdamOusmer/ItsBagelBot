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

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

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
	p.view(b, "stats", func(tag, msg string) any { return statsReply{Tag: tag, Error: msg} }, shapeStats)
	p.view(b, "decks", func(tag, msg string) any { return decksReply{Tag: tag, Error: msg} }, shapeDecks)
	p.view(b, "ranked", func(tag, msg string) any { return rankedReply{Tag: tag, Error: msg} }, shapeRanked)
	p.view(b, "trophy_road", func(tag, msg string) any { return trophyRoadReply{Tag: tag, Error: msg} }, shapeTrophyRoad)
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
		buckets: core.NewBuckets("ratelimit:gateway:clashroyale", cfg.RateLimit, rateWindowSeconds),
	}
}

// view declares one byte-flow endpoint that projects the shared player profile
// through shape.
func (p *api) view(b *provider.Builder, name string, errReply provider.ReplyFunc, shape func(playerProfile) any) {
	b.Endpoint(name).Timeout(handlerTimeout).
		Cached(profileTTL, negativeTTL).
		ID(tagID).
		Reply(errReply).
		Fallback(name + " lookup failed").
		Fetch(p.profileFetch(shape))
}

// tagID validates and canonicalizes the player tag: the reply echoes the
// canonical "#TAG" form once it parses, or the raw input on a validation
// error.
func tagID(req gatewayrpc.Request) (provider.ID, string) {
	tag, msg := parsePlayerTag(req.Account)
	if msg != "" {
		return provider.ID{Display: strings.TrimSpace(req.Account)}, msg
	}
	return provider.ID{Display: tag.String(), Key: tag.cacheKey()}, ""
}

// profileFetch loads the shared profile and projects it through shape.
func (p *api) profileFetch(shape func(playerProfile) any) provider.FetchFunc {
	return func(ctx context.Context, req gatewayrpc.Request, id provider.ID) (any, error) {
		tag, _ := parsePlayerTag(id.Display) // already validated by tagID
		profile, err := p.profile(ctx, tag, req.IsPremium)
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
// four views. Unknown upstream additions are ignored by encoding/json.
type playerProfile struct {
	Tag                       string       `json:"tag"`
	Name                      string       `json:"name"`
	ExpLevel                  int          `json:"expLevel"`
	ExpPoints                 int64        `json:"expPoints"`
	StarPoints                int64        `json:"starPoints"`
	Trophies                  int          `json:"trophies"`
	BestTrophies              int          `json:"bestTrophies"`
	Wins                      int          `json:"wins"`
	Losses                    int          `json:"losses"`
	BattleCount               int          `json:"battleCount"`
	ThreeCrownWins            int          `json:"threeCrownWins"`
	ChallengeCardsWon         int          `json:"challengeCardsWon"`
	ChallengeMaxWins          int          `json:"challengeMaxWins"`
	TournamentCardsWon        int          `json:"tournamentCardsWon"`
	TournamentBattleCount     int          `json:"tournamentBattleCount"`
	Donations                 int          `json:"donations"`
	DonationsReceived         int          `json:"donationsReceived"`
	TotalDonations            int          `json:"totalDonations"`
	Arena                     arena        `json:"arena"`
	Clan                      clan         `json:"clan"`
	CurrentFavouriteCard      card         `json:"currentFavouriteCard"`
	CurrentDeck               []card       `json:"currentDeck"`
	CurrentDeckSupportCards   []card       `json:"currentDeckSupportCards"`
	LeagueStatistics          leagueStats  `json:"leagueStatistics"`
	CurrentPathOfLegendResult rankedResult `json:"currentPathOfLegendSeasonResult"`
	LastPathOfLegendResult    rankedResult `json:"lastPathOfLegendSeasonResult"`
	BestPathOfLegendResult    rankedResult `json:"bestPathOfLegendSeasonResult"`
}

type arena struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type clan struct {
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	BadgeID int64  `json:"badgeId,omitempty"`
}

type iconURLs struct {
	Medium    string `json:"medium,omitempty"`
	Evolution string `json:"evolutionMedium,omitempty"`
}

type card struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Level             int      `json:"level,omitempty"`
	MaxLevel          int      `json:"maxLevel,omitempty"`
	EvolutionLevel    int      `json:"evolutionLevel,omitempty"`
	MaxEvolutionLevel int      `json:"maxEvolutionLevel,omitempty"`
	ElixirCost        int      `json:"elixirCost,omitempty"`
	Rarity            string   `json:"rarity,omitempty"`
	IconURLs          iconURLs `json:"iconUrls,omitempty"`
}

// rankedResult covers both Path of Legends results and the legacy league
// season records. Fields absent in one representation remain zero-valued.
type rankedResult struct {
	SeasonID     string `json:"id,omitempty"`
	LeagueNumber int    `json:"leagueNumber,omitempty"`
	Trophies     int    `json:"trophies,omitempty"`
	BestTrophies int    `json:"bestTrophies,omitempty"`
	Rank         int    `json:"rank,omitempty"`
}

type leagueStats struct {
	Current  rankedResult `json:"currentSeason"`
	Previous rankedResult `json:"previousSeason"`
	Best     rankedResult `json:"bestSeason"`
}

// Public endpoint replies live in the provider package intentionally: this
// change adds only the gateway integration and no sesame-facing feature.
type statsReply struct {
	Player                string  `json:"player"`
	Tag                   string  `json:"tag"`
	KingLevel             int     `json:"king_level"`
	ExperiencePoints      int64   `json:"experience_points"`
	StarPoints            int64   `json:"star_points"`
	Wins                  int     `json:"wins"`
	Losses                int     `json:"losses"`
	Draws                 int     `json:"draws"`
	Battles               int     `json:"battles"`
	WinRate               float64 `json:"win_rate"`
	ThreeCrownWins        int     `json:"three_crown_wins"`
	ChallengeCardsWon     int     `json:"challenge_cards_won"`
	ChallengeMaxWins      int     `json:"challenge_max_wins"`
	TournamentCardsWon    int     `json:"tournament_cards_won"`
	TournamentBattleCount int     `json:"tournament_battle_count"`
	Donations             int     `json:"donations"`
	DonationsReceived     int     `json:"donations_received"`
	TotalDonations        int     `json:"total_donations"`
	Clan                  clan    `json:"clan"`
	FavouriteCard         card    `json:"favourite_card"`
	Error                 string  `json:"error,omitempty"`
}

type decksReply struct {
	Player        string  `json:"player"`
	Tag           string  `json:"tag"`
	CurrentDeck   []card  `json:"current_deck"`
	SupportCards  []card  `json:"support_cards"`
	AverageElixir float64 `json:"average_elixir"`
	Error         string  `json:"error,omitempty"`
}

type rankedReply struct {
	Player   string       `json:"player"`
	Tag      string       `json:"tag"`
	Current  rankedResult `json:"current"`
	Previous rankedResult `json:"previous"`
	Best     rankedResult `json:"best"`
	Unranked bool         `json:"unranked"`
	Error    string       `json:"error,omitempty"`
}

type trophyRoadReply struct {
	Player       string `json:"player"`
	Tag          string `json:"tag"`
	Trophies     int    `json:"trophies"`
	BestTrophies int    `json:"best_trophies"`
	Arena        arena  `json:"arena"`
	Error        string `json:"error,omitempty"`
}

func (p *api) profile(ctx context.Context, tag playerTag, isPremium bool) (playerProfile, error) {
	key := core.Key(providerName, "profile", tag.cacheKey())
	return core.Cached(ctx, p.cache, key, profileTTL, negativeTTL, func(ctx context.Context) (playerProfile, error) {
		if err := p.buckets.Enforce(ctx, p.limiter, isPremium); err != nil {
			return playerProfile{}, err
		}
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
	return statsReply{
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
	return decksReply{
		Player: profile.Name, Tag: profile.Tag, CurrentDeck: profile.CurrentDeck,
		SupportCards: profile.CurrentDeckSupportCards, AverageElixir: average,
	}
}

func hasRankedResult(r rankedResult) bool {
	return r.SeasonID != "" || r.LeagueNumber != 0 || r.Trophies != 0 || r.BestTrophies != 0 || r.Rank != 0
}

func preferRanked(primary, fallback rankedResult) rankedResult {
	if hasRankedResult(primary) {
		return primary
	}
	return fallback
}

func shapeRanked(profile playerProfile) any {
	current := preferRanked(profile.CurrentPathOfLegendResult, profile.LeagueStatistics.Current)
	previous := preferRanked(profile.LastPathOfLegendResult, profile.LeagueStatistics.Previous)
	best := preferRanked(profile.BestPathOfLegendResult, profile.LeagueStatistics.Best)
	return rankedReply{
		Player: profile.Name, Tag: profile.Tag, Current: current, Previous: previous, Best: best,
		Unranked: !hasRankedResult(current),
	}
}

func shapeTrophyRoad(profile playerProfile) any {
	return trophyRoadReply{
		Player: profile.Name, Tag: profile.Tag, Trophies: profile.Trophies,
		BestTrophies: profile.BestTrophies, Arena: profile.Arena,
	}
}
