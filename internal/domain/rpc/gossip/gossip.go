// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gossiprpc

type Request struct {
	Account   string `json:"account"`
	ChannelID string `json:"channel_id,omitempty"`
	IsPremium bool   `json:"is_premium,omitempty"`

	Code        string `json:"code,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`

	Device   string `json:"device,omitempty"`
	SKU      string `json:"sku,omitempty"`
	ColorRGB int    `json:"color_rgb,omitempty"`
	PowerOff bool   `json:"power_off,omitempty"`

	Query    string `json:"query,omitempty"`
	TrackID  string `json:"track_id,omitempty"`
	ArtistID string `json:"artist_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`

	AccountType string `json:"account_type,omitempty"`
	TimeWindow  string `json:"time_window,omitempty"`

	HoursBetween int `json:"hours_between,omitempty"`

	Season    int    `json:"season,omitempty"`
	AccountB  string `json:"account_b,omitempty"`
	Board     string `json:"board,omitempty"`
	Predicted bool   `json:"predicted,omitempty"`
	Country   string `json:"country,omitempty"`

	DefID  string    `json:"def_id,omitempty"`
	Def    *FetchDef `json:"def,omitempty"`
	DryRun bool      `json:"dry_run,omitempty"`
	Fresh  bool      `json:"fresh,omitempty"`

	Region   string `json:"region,omitempty"`
	Platform string `json:"platform,omitempty"`
}

func Subject(prefix, provider, endpoint string) string {
	return prefix + "." + provider + "." + endpoint
}

type UrchinSessionReply struct {
	Player      string `json:"player"`
	SinceUnix   int64  `json:"since_unix"`
	Wins        int64  `json:"wins"`
	Losses      int64  `json:"losses"`
	FinalKills  int64  `json:"final_kills"`
	FinalDeaths int64  `json:"final_deaths"`
	BedsBroken  int64  `json:"beds_broken"`
	GamesPlayed int64  `json:"games_played"`
	Levels      int64  `json:"levels"`
	Error       string `json:"error,omitempty"`
}

type UrchinStatsReply struct {
	Player      string `json:"player"`
	Stars       int64  `json:"stars"`
	Wins        int64  `json:"wins"`
	Losses      int64  `json:"losses"`
	FinalKills  int64  `json:"final_kills"`
	FinalDeaths int64  `json:"final_deaths"`
	BedsBroken  int64  `json:"beds_broken"`
	Error       string `json:"error,omitempty"`
}

type HypixelStatsReply struct {
	Player      string `json:"player"`
	Stars       int64  `json:"stars"`
	Wins        int64  `json:"wins"`
	Losses      int64  `json:"losses"`
	FinalKills  int64  `json:"final_kills"`
	FinalDeaths int64  `json:"final_deaths"`
	BedsBroken  int64  `json:"beds_broken"`
	Error       string `json:"error,omitempty"`
}

type HypixelUUIDReply struct {
	Player string `json:"player"`
	UUID   string `json:"uuid"`
	Error  string `json:"error,omitempty"`
}

type UrchinSniperReply struct {
	Player   string  `json:"player"`
	Score    float64 `json:"score"`
	Mode     string  `json:"mode"`
	TagCount int     `json:"tag_count"`
	Error    string  `json:"error,omitempty"`
}

type UrchinTag struct {
	Type    string `json:"type"`
	Reason  string `json:"reason,omitempty"`
	AddedOn int64  `json:"added_on,omitempty"`
}

type UrchinTagsReply struct {
	Player string      `json:"player"`
	Tags   []UrchinTag `json:"tags"`
	Error  string      `json:"error,omitempty"`
}

type McsrUserReply struct {
	Nickname   string `json:"nickname"`
	UUID       string `json:"uuid"`
	Elo        int    `json:"elo"`
	Rank       int    `json:"rank"`
	Country    string `json:"country,omitempty"`
	Wins       int    `json:"wins"`
	Loses      int    `json:"loses"`
	Played     int    `json:"played"`
	BestTimeMS int64  `json:"best_time_ms"`
	Error      string `json:"error,omitempty"`
}

type McsrSnapshotReply struct {
	Nickname string `json:"nickname"`
	Elo      int    `json:"elo"`
	Error    string `json:"error,omitempty"`
}

type CODMProfileReply struct {
	Player    string `json:"player"`
	Level     int    `json:"level"`
	Rank      string `json:"rank"`
	RankClass int    `json:"rank_class"`
	Rating    int    `json:"rating"`
	Country   string `json:"country"`
	ShortID   string `json:"short_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type FortniteModeStats struct {
	Wins    int64   `json:"wins"`
	Matches int64   `json:"matches"`
	Kills   int64   `json:"kills"`
	KD      float64 `json:"kd"`
	WinRate float64 `json:"win_rate"`
}

type FortniteStatsReply struct {
	Player  string            `json:"player"`
	Window  string            `json:"window"`
	Overall FortniteModeStats `json:"overall"`
	Solo    FortniteModeStats `json:"solo"`
	Duo     FortniteModeStats `json:"duo"`
	Squad   FortniteModeStats `json:"squad"`
	Error   string            `json:"error,omitempty"`
}

type FortniteShopEntry struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
}

type FortniteShopReply struct {
	Date    string              `json:"date"`
	Count   int                 `json:"count"`
	Entries []FortniteShopEntry `json:"entries"`
	Error   string              `json:"error,omitempty"`
}

type FortniteSnapshotReply struct {
	Player string `json:"player"`
	Error  string `json:"error,omitempty"`
}

type FortniteSessionReply struct {
	Player      string  `json:"player"`
	Wins        int64   `json:"wins"`
	Matches     int64   `json:"matches"`
	Kills       int64   `json:"kills"`
	KD          float64 `json:"kd"`
	WinRate     float64 `json:"win_rate"`
	SinceUnix   int64   `json:"since_unix"`
	HasSnapshot bool    `json:"has_snapshot"`
	Error       string  `json:"error,omitempty"`
}

type GoveeDevice struct {
	Device string `json:"device"`
	SKU    string `json:"sku"`
	Name   string `json:"name"`
	Color  bool   `json:"color"`
}

type GoveeDevicesReply struct {
	Devices []GoveeDevice `json:"devices"`
	Error   string        `json:"error,omitempty"`
}

type GoveeControlReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type SpotifyTrack struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	DurationMS int64    `json:"duration_ms"`
	ImageURL   string   `json:"image_url,omitempty"`
	URL        string   `json:"url,omitempty"`
}

type SpotifySearchReply struct {
	Tracks     []SpotifyTrack `json:"tracks"`
	ResolvedAs string         `json:"resolved_as,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type SpotifyTrackReply struct {
	Track *SpotifyTrack `json:"track,omitempty"`
	Error string        `json:"error,omitempty"`
}

type SpotifyArtist struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Genres    []string `json:"genres,omitempty"`
	Followers int64    `json:"followers"`
	ImageURL  string   `json:"image_url,omitempty"`
	URL       string   `json:"url,omitempty"`
}

type SpotifyArtistReply struct {
	Artist *SpotifyArtist `json:"artist,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type SpotifyNowPlayingReply struct {
	IsPlaying  bool          `json:"is_playing"`
	ProgressMS int64         `json:"progress_ms,omitempty"`
	Track      *SpotifyTrack `json:"track,omitempty"`
	Error      string        `json:"error,omitempty"`
}

type SpotifyExchangeReply struct {
	RefreshToken string   `json:"refresh_token,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type SpotifyPlayerReply struct {
	Error string `json:"error,omitempty"`
}

type McsrSessionReply struct {
	Nickname    string `json:"nickname"`
	Elo         int    `json:"elo"`
	EloChange   int    `json:"elo_change"`
	Wins        int    `json:"wins"`
	Loses       int    `json:"loses"`
	Played      int    `json:"played"`
	SinceUnix   int64  `json:"since_unix"`
	HasSnapshot bool   `json:"has_snapshot"`
	Error       string `json:"error,omitempty"`
}

type PacemanSessionReply struct {
	Player          string  `json:"player"`
	NetherCount     int     `json:"nether_count"`
	Nether          string  `json:"nether"`
	Bastion         string  `json:"bastion"`
	Fortress        string  `json:"fortress"`
	FirstStructure  string  `json:"first_structure"`
	SecondStructure string  `json:"second_structure"`
	FirstPortal     string  `json:"first_portal"`
	Stronghold      string  `json:"stronghold"`
	End             string  `json:"end"`
	Finish          string  `json:"finish"`
	NPH             float64 `json:"nph"`
	Empty           bool    `json:"empty"`
	Error           string  `json:"error,omitempty"`
}

type PacemanNethersReply struct {
	Player string  `json:"player"`
	Count  int     `json:"count"`
	Avg    string  `json:"avg"`
	NPH    float64 `json:"nph"`
	Empty  bool    `json:"empty"`
	Error  string  `json:"error,omitempty"`
}

type PacemanLastFortReply struct {
	Player      string `json:"player"`
	Nether      string `json:"nether"`
	Bastion     string `json:"bastion"`
	Fortress    string `json:"fortress"`
	FirstPortal string `json:"first_portal"`
	Stronghold  string `json:"stronghold"`
	End         string `json:"end"`
	Finish      string `json:"finish"`
	AgoSeconds  int64  `json:"ago_seconds"`
	Empty       bool   `json:"empty"`
	Error       string `json:"error,omitempty"`
}

type PacemanPersonalBestReply struct {
	Player string `json:"player"`
	Window string `json:"window"`
	Time   string `json:"time"`
	Empty  bool   `json:"empty"`
	Error  string `json:"error,omitempty"`
}

type McsrLastMatchReply struct {
	Player     string `json:"player"`
	Opponent   string `json:"opponent"`
	Result     string `json:"result"`
	Time       string `json:"time"`
	Seed       string `json:"seed"`
	Structure  string `json:"structure"`
	EloChange  int    `json:"elo_change"`
	AgoSeconds int64  `json:"ago_seconds"`
	Forfeited  bool   `json:"forfeited"`
	Decayed    bool   `json:"decayed"`
	Empty      bool   `json:"empty"`
	Error      string `json:"error,omitempty"`
}

type McsrRecordReply struct {
	PlayerA string `json:"player_a"`
	PlayerB string `json:"player_b"`
	WinsA   int    `json:"wins_a"`
	WinsB   int    `json:"wins_b"`
	Played  int    `json:"played"`
	Error   string `json:"error,omitempty"`
}

type McsrLeaderboardEntry struct {
	Rank  int    `json:"rank"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type McsrLeaderboardReply struct {
	Board   string                 `json:"board"`
	Entries []McsrLeaderboardEntry `json:"entries"`
	Empty   bool                   `json:"empty"`
	Error   string                 `json:"error,omitempty"`
}

type McsrWeeklyRaceReply struct {
	LeaderName string `json:"leader_name"`
	LeaderTime string `json:"leader_time"`
	Player     string `json:"player"`
	PlayerTime string `json:"player_time"`
	PlayerRank int    `json:"player_rank"`
	HasPlayer  bool   `json:"has_player"`
	Empty      bool   `json:"empty"`
	Error      string `json:"error,omitempty"`
}

type ClashRoyaleArena struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ClashRoyaleClan struct {
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	BadgeID int64  `json:"badgeId,omitempty"`
}

type ClashRoyaleCardIconURLs struct {
	Medium    string `json:"medium,omitempty"`
	Evolution string `json:"evolutionMedium,omitempty"`
}

type ClashRoyaleCard struct {
	ID                int64                   `json:"id"`
	Name              string                  `json:"name"`
	Level             int                     `json:"level,omitempty"`
	MaxLevel          int                     `json:"maxLevel,omitempty"`
	EvolutionLevel    int                     `json:"evolutionLevel,omitempty"`
	MaxEvolutionLevel int                     `json:"maxEvolutionLevel,omitempty"`
	ElixirCost        int                     `json:"elixirCost,omitempty"`
	Rarity            string                  `json:"rarity,omitempty"`
	IconURLs          ClashRoyaleCardIconURLs `json:"iconUrls,omitempty"`
}

type ClashRoyaleRankedResult struct {
	SeasonID     string `json:"id,omitempty"`
	LeagueNumber int    `json:"leagueNumber,omitempty"`
	Trophies     int    `json:"trophies,omitempty"`
	BestTrophies int    `json:"bestTrophies,omitempty"`
	Rank         int    `json:"rank,omitempty"`
}

type ClashRoyaleStatsReply struct {
	Player                string          `json:"player"`
	Tag                   string          `json:"tag"`
	KingLevel             int             `json:"king_level"`
	ExperiencePoints      int64           `json:"experience_points"`
	StarPoints            int64           `json:"star_points"`
	Wins                  int             `json:"wins"`
	Losses                int             `json:"losses"`
	Draws                 int             `json:"draws"`
	Battles               int             `json:"battles"`
	WinRate               float64         `json:"win_rate"`
	ThreeCrownWins        int             `json:"three_crown_wins"`
	ChallengeCardsWon     int             `json:"challenge_cards_won"`
	ChallengeMaxWins      int             `json:"challenge_max_wins"`
	TournamentCardsWon    int             `json:"tournament_cards_won"`
	TournamentBattleCount int             `json:"tournament_battle_count"`
	Donations             int             `json:"donations"`
	DonationsReceived     int             `json:"donations_received"`
	TotalDonations        int             `json:"total_donations"`
	Clan                  ClashRoyaleClan `json:"clan"`
	FavouriteCard         ClashRoyaleCard `json:"favourite_card"`
	Error                 string          `json:"error,omitempty"`
}

type ClashRoyaleDecksReply struct {
	Player        string            `json:"player"`
	Tag           string            `json:"tag"`
	CurrentDeck   []ClashRoyaleCard `json:"current_deck"`
	SupportCards  []ClashRoyaleCard `json:"support_cards"`
	AverageElixir float64           `json:"average_elixir"`
	Error         string            `json:"error,omitempty"`
}

type ClashRoyaleRankedReply struct {
	Player   string                  `json:"player"`
	Tag      string                  `json:"tag"`
	Current  ClashRoyaleRankedResult `json:"current"`
	Previous ClashRoyaleRankedResult `json:"previous"`
	Best     ClashRoyaleRankedResult `json:"best"`
	Unranked bool                    `json:"unranked"`
	Error    string                  `json:"error,omitempty"`
}

type ClashRoyaleTrophyRoadReply struct {
	Player       string           `json:"player"`
	Tag          string           `json:"tag"`
	Trophies     int              `json:"trophies"`
	BestTrophies int              `json:"best_trophies"`
	Arena        ClashRoyaleArena `json:"arena"`
	Error        string           `json:"error,omitempty"`
}

type ValorantRankReply struct {
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

type ValorantMatchEntry struct {
	Map        string  `json:"map"`
	Agent      string  `json:"agent"`
	Result     string  `json:"result"`
	Kills      int     `json:"kills"`
	Deaths     int     `json:"deaths"`
	Assists    int     `json:"assists"`
	ACS        float64 `json:"acs"`
	AgoSeconds int64   `json:"ago_seconds"`
}

type ValorantMatchesReply struct {
	Player  string               `json:"player"`
	Region  string               `json:"region"`
	Matches []ValorantMatchEntry `json:"matches"`
	Empty   bool                 `json:"empty"`
	Error   string               `json:"error,omitempty"`
}

type ValorantAccountReply struct {
	Player       string `json:"player"`
	Puuid        string `json:"puuid,omitempty"`
	Region       string `json:"region,omitempty"`
	AccountLevel int    `json:"account_level,omitempty"`
	Card         string `json:"card,omitempty"`
	Title        string `json:"title,omitempty"`
	Error        string `json:"error,omitempty"`
}

type ValorantLeaderboardEntry struct {
	Rank   int    `json:"rank"`
	Player string `json:"player"`
	Tier   int    `json:"tier"`
	RR     int    `json:"rr"`
	Wins   int    `json:"wins"`
}

type ValorantLeaderboardReply struct {
	Player  string                     `json:"player,omitempty"`
	Board   string                     `json:"board,omitempty"`
	Entries []ValorantLeaderboardEntry `json:"entries"`
	Empty   bool                       `json:"empty"`
	Error   string                     `json:"error,omitempty"`
}

type ValorantShopItem struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
	Tier  string `json:"tier,omitempty"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type ValorantShopReply struct {
	ResetUnix int64              `json:"reset_unix"`
	Items     []ValorantShopItem `json:"items"`
	Count     int                `json:"count"`
	Empty     bool               `json:"empty"`
	Error     string             `json:"error,omitempty"`
}

type FetchDef struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	JSONPath []string `json:"json_path,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"`
	IsActive bool     `json:"is_active"`
}

type FetchStatus string

const (
	FetchOK            FetchStatus = "ok"
	FetchDenied        FetchStatus = "denied"
	FetchLimited       FetchStatus = "limited"
	FetchUpstreamError FetchStatus = "upstream_error"
	FetchTimeout       FetchStatus = "timeout"
	FetchBadDef        FetchStatus = "bad_def"
)

type CustomFetchReply struct {
	Status FetchStatus `json:"status"`
	Values []string    `json:"values,omitempty"`
	MS     int         `json:"ms"`
	Error  string      `json:"error,omitempty"`
	Sample string      `json:"sample,omitempty"`
}
