// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package gossiprpc holds the shared wire types for the gossip service RPC
// surface: the one request shape every provider endpoint accepts, and the typed
// reply each endpoint answers with.
//
// The gossip service proxies and caches external API systems (urchin.gg, MCSR Ranked,
// ...) behind NATS request/reply so no chat-path service ever dials the
// internet itself. Subjects are "<prefix>.<provider>.<endpoint>" (default
// prefix "bagel.rpc.gossip"), e.g. "bagel.rpc.gossip.urchin.daily".
//
// Every reply embeds the fleet's conventional {"error": ""} envelope, so
// bus.RequestJSON callers get a Go error (bus.RPCReplyError) instead of a
// zero-valued success when the provider answered with a failure such as
// "player not found".
package gossiprpc

// Request covers every gossip endpoint; unused fields are zero.
type Request struct {
	// Account is the provider-side account the lookup targets (a Minecraft
	// username or UUID for urchin/mcsr).
	Account string `json:"account"`
	// ChannelID scopes session-stateful endpoints (mcsr session snapshots) to
	// one broadcaster, so two channels tracking the same player never share a
	// stream session. The govee provider also reads it as the broadcaster whose
	// stored (encrypted) Govee API key the call authenticates with.
	ChannelID string `json:"channel_id,omitempty"`
	// IsPremium indicates whether the caller is on the premium lane, enabling
	// the provider to consume from the reserved premium rate limit bucket.
	IsPremium bool `json:"is_premium,omitempty"`

	// --- govee (per-broadcaster smart-light control) ------------------------
	// The govee provider authenticates with the broadcaster's own stored key
	// (resolved from ChannelID), so these carry only which device to act on and
	// the colour to set. Zero on every non-govee request.

	// Device is the Govee device id (its MAC-style address) govee.control acts
	// on; empty on govee.devices (which lists them).
	Device string `json:"device,omitempty"`
	// SKU is the device model (e.g. "H6159") the Govee control payload pairs
	// with Device.
	SKU string `json:"sku,omitempty"`
	// ColorRGB is the packed 24-bit colour (r<<16|g<<8|b) govee.control sets.
	// Range 0..0xFFFFFF; the caller rejects an unparseable colour before the
	// call, so 0 (sent as omitted) only reaches control as a deliberate black.
	ColorRGB int `json:"color_rgb,omitempty"`
	// PowerOff, when true, makes govee.control turn the device OFF instead of
	// powering it on and setting a colour; ColorRGB is ignored. This backs the
	// opt-in "a viewer types off to turn the lights off" reward behaviour.
	PowerOff bool `json:"power_off,omitempty"`

	// --- fortnite (fortnite-api.com stats lookups) ---------------------------

	// AccountType is the platform namespace Account lives in for fortnite.stats:
	// "epic" (default), "psn" or "xbl". Zero on every non-fortnite request.
	AccountType string `json:"account_type,omitempty"`
	// TimeWindow selects a provider's answer window. On fortnite.stats:
	// "lifetime" (default) or "season" (the current season only). On
	// paceman.personal_best: "" (all-time, default), "daily", "weekly" or
	// "monthly" — the same four buckets PaceMan itself precomputes per
	// player, so the provider never aggregates a window client-side.
	TimeWindow string `json:"time_window,omitempty"`

	// --- paceman (PaceMan.gg Minecraft speedrun pace tracking) ---------------

	// HoursBetween is the session-cutoff gap paceman.session/paceman.nethers
	// pass upstream as hoursBetween: how long a player can go without starting a
	// new run before PaceMan calls the session over. Zero (the common case) lets
	// the provider apply its own default rather than every caller repeating it.
	HoursBetween int `json:"hours_between,omitempty"`

	// --- mcsr (MCSR Ranked: match history, versus, leaderboards, weekly race) ---

	// Season scopes a lookup to one MCSR Ranked season instead of the current
	// one. Shared by !elo, !lastmatch, !record and !lb (mcsr.user,
	// mcsr.last_match, mcsr.versus, mcsr.leaderboard) since the upstream
	// accepts the same season param on all of them; zero means "current
	// season", the upstream's own default, so an unset Season never needs a
	// provider-side lookup of what "current" means.
	Season int `json:"season,omitempty"`
	// AccountB is the second player for mcsr.versus (!record): Account is the
	// first. Zero on every other request.
	AccountB string `json:"account_b,omitempty"`
	// Board selects which of mcsr.leaderboard's three upstream endpoints
	// answers !lb: "" (default) is the elo leaderboard, "phase" is the
	// phase-point leaderboard, "record" is the season-best-time leaderboard.
	Board string `json:"board,omitempty"`
	// Predicted asks mcsr.leaderboard's phase board for predicted phase
	// points instead of locked-in ones (upstream only honors this for the
	// current season). Ignored on every other board.
	Predicted bool `json:"predicted,omitempty"`
	// Country filters mcsr.leaderboard to one lowercase ISO-3166-1 alpha-2
	// code. The upstream record-leaderboard has no such filter, so the
	// provider drops it silently on that board rather than erroring.
	Country string `json:"country,omitempty"`

	// --- valorant (HenrikDev community API lookups) --------------------------

	// Region is the Valorant shard a lookup targets: "na", "eu", "ap", "kr",
	// "br" or "latam". Empty lets the provider detect it from the account
	// itself through one extra cached account lookup, so callers that don't
	// know their shard stay one upstream read behind explicit ones only on the
	// first ask of the day. Zero on every non-valorant request.
	Region string `json:"region,omitempty"`
	// Platform selects which ladder split answers: "pc" (the provider's
	// default when empty) or "console". Leaderboards and MMR are tracked as
	// separate ladders per platform, so this folds into cache keys rather
	// than being normalized away. Zero on every non-valorant request.
	Platform string `json:"platform,omitempty"`
}

// Subject builds the NATS subject for one provider endpoint under prefix.
func Subject(prefix, provider, endpoint string) string {
	return prefix + "." + provider + "." + endpoint
}

// --- urchin (Coral: Hypixel Bed Wars stats + Urchin blacklist) --------------

// UrchinSessionReply is the answer to urchin.daily / urchin.weekly /
// urchin.monthly: the change in a player's Bed Wars stats since the period's
// reset.
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

// UrchinStatsReply is the answer to urchin.stats: the player's lifetime Bed
// Wars stats extracted from their Hypixel profile.
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

// --- hypixel (direct Hypixel API) --------------------------------------------

// HypixelStatsReply is the answer to hypixel.stats: the player's lifetime Bed
// Wars stats read straight from the Hypixel API. It is the wire the urchin
// dashboard module's !bwstats rides — same shape as UrchinStatsReply, owned by
// the hypixel provider (Coral's profile endpoint needs a key permission ours
// does not carry, so lifetime stats bypass Coral entirely).
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

// UrchinSniperReply is the answer to urchin.sniper: the player's Urchin
// (Cubelify overlay) sniper score.
type UrchinSniperReply struct {
	Player string `json:"player"`
	// Score is the overlay score value; Mode describes how the overlay should
	// interpret it (as returned by the API).
	Score    float64 `json:"score"`
	Mode     string  `json:"mode"`
	TagCount int     `json:"tag_count"`
	Error    string  `json:"error,omitempty"`
}

// UrchinTag is one active blacklist tag on a player.
type UrchinTag struct {
	Type    string `json:"type"`
	Reason  string `json:"reason,omitempty"`
	AddedOn int64  `json:"added_on,omitempty"`
}

// UrchinTagsReply is the answer to urchin.tags: the blacklist tags currently
// active on a player.
type UrchinTagsReply struct {
	Player string      `json:"player"`
	Tags   []UrchinTag `json:"tags"`
	Error  string      `json:"error,omitempty"`
}

// --- mcsr (MCSR Ranked) ------------------------------------------------------

// McsrUserReply is the answer to mcsr.user: the player's current MCSR Ranked
// standing. Elo and Rank are -1 when the player is unrated this season.
type McsrUserReply struct {
	Nickname string `json:"nickname"`
	UUID     string `json:"uuid"`
	Elo      int    `json:"elo"`
	Rank     int    `json:"rank"`
	Country  string `json:"country,omitempty"`
	// Season counters (ranked queue).
	Wins   int `json:"wins"`
	Loses  int `json:"loses"`
	Played int `json:"played"`
	// BestTimeMS is the season's best ranked completion in milliseconds; 0 when
	// none.
	BestTimeMS int64  `json:"best_time_ms"`
	Error      string `json:"error,omitempty"`
}

// McsrSnapshotReply is the answer to mcsr.session_start: acknowledges the
// stream-start snapshot the session delta is later computed against.
type McsrSnapshotReply struct {
	Nickname string `json:"nickname"`
	Elo      int    `json:"elo"`
	Error    string `json:"error,omitempty"`
}

// --- fortnite (fortnite-api.com) ---------------------------------------------

// FortniteModeStats is one queue's normalized Battle Royale counters inside
// FortniteStatsReply: the bot-needed subset (wins, matches, kills, K/D, win
// rate) of the much larger upstream stat block. KD and WinRate come from the
// upstream pre-computed; WinRate is a percentage (4.83 = 4.83%).
type FortniteModeStats struct {
	Wins    int64   `json:"wins"`
	Matches int64   `json:"matches"`
	Kills   int64   `json:"kills"`
	KD      float64 `json:"kd"`
	WinRate float64 `json:"win_rate"`
}

// FortniteStatsReply is the answer to fortnite.stats (sesame's !fnstats): a
// player's Battle Royale stats over the requested window, overall plus the
// solo/duo/squad mode breakdown. Window echoes the normalized window the
// gateway actually queried ("lifetime" or "season").
type FortniteStatsReply struct {
	Player  string            `json:"player"`
	Window  string            `json:"window"`
	Overall FortniteModeStats `json:"overall"`
	Solo    FortniteModeStats `json:"solo"`
	Duo     FortniteModeStats `json:"duo"`
	Squad   FortniteModeStats `json:"squad"`
	Error   string            `json:"error,omitempty"`
}

// FortniteShopEntry is one item-shop offer reduced to what a chat line can
// carry: a display name (the bundle name, or the lead item's) and the final
// price in V-Bucks.
type FortniteShopEntry struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
}

// FortniteShopReply is the answer to fortnite.shop (sesame's !store): the
// current item-shop rotation. Date is the rotation day (YYYY-MM-DD).
type FortniteShopReply struct {
	Date    string              `json:"date"`
	Count   int                 `json:"count"`
	Entries []FortniteShopEntry `json:"entries"`
	Error   string              `json:"error,omitempty"`
}

// FortniteSnapshotReply is the answer to fortnite.session_start: acknowledges
// the stream-start snapshot the session delta is later computed against.
type FortniteSnapshotReply struct {
	Player string `json:"player"`
	Error  string `json:"error,omitempty"`
}

// FortniteSessionReply is the answer to fortnite.session (sesame's !fn
// session): the change in a player's Battle Royale stats since the
// stream-start snapshot for this channel. Wins/Matches/Kills are deltas over
// the live session; KD and WinRate are derived from those session games alone.
//
// HasSnapshot is false when no snapshot existed for the channel (the module
// was enabled mid-stream, or the gossip service lost it); the gossip service then takes one,
// so the next call has a baseline.
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

// --- govee (smart-light control over the broadcaster's own key) -------------

// GoveeDevice is one controllable device on a broadcaster's Govee account, as
// govee.devices lists it for the dashboard's device picker.
type GoveeDevice struct {
	// Device is the device id (MAC-style address) control calls target.
	Device string `json:"device"`
	// SKU is the model code paired with Device in a control payload.
	SKU string `json:"sku"`
	// Name is the user-facing device name set in the Govee app.
	Name string `json:"name"`
	// Color reports whether the device advertises the RGB colour capability, so
	// the picker can hide lights the color reward could never drive.
	Color bool `json:"color"`
}

// GoveeDevicesReply is the answer to govee.devices: the broadcaster's
// controllable devices. A missing/invalid key surfaces as Error so the
// dashboard can prompt the broadcaster to re-enter it.
type GoveeDevicesReply struct {
	Devices []GoveeDevice `json:"devices"`
	Error   string        `json:"error,omitempty"`
}

// GoveeControlReply is the answer to govee.control: it acknowledges that the
// device was powered on and set to the requested colour, or reports why not.
type GoveeControlReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// McsrSessionReply is the answer to mcsr.session: the change in a player's
// ranked standing since the stream-start snapshot for this channel.
//
// HasSnapshot is false when no snapshot existed for the channel (module was
// enabled mid-stream, or the gossip service lost it); the gossip service then takes one, so
// the next call has a baseline.
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

// --- paceman (PaceMan.gg Minecraft speedrun pace tracking) -------------------
//
// PaceMan is an independent upstream from MCSR Ranked: it tracks in-progress
// speedrun splits (nether, bastion/fortress, portal, stronghold, end) rather
// than ranked match results, so it gets its own gossip provider and its own
// cache/rate-limit budget. The commands it answers still hang off sesame's
// existing mcsr module and reuse that module's linked account, since from a
// broadcaster's perspective "which Minecraft player" is one setting either
// way.

// PacemanSessionReply is the answer to paceman.session (sesame's !pace): split
// averages and nether count over the current rolling session window, plus
// nethers-per-hour when the player runs the PaceMan Tracker. Avg strings are
// PaceMan's own pre-formatted "m:ss"; Empty is true when the player has no
// pace tracked this window (NetherCount is 0), which is a normal answer, not
// an error.
type PacemanSessionReply struct {
	Player      string `json:"player"`
	NetherCount int    `json:"nether_count"`
	Nether      string `json:"nether"`
	Bastion     string `json:"bastion"`
	Fortress    string `json:"fortress"`
	// Structure-order averages: first/second structure entered, whichever it was.
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

// PacemanNethersReply is the answer to paceman.nethers (sesame's !nethers):
// just the nether-entrance count and pace for the session, the subset of
// PacemanSessionReply that command needs. NPH is 0 (and Empty true only when
// Count is also 0) for a player who does not run the PaceMan Tracker.
type PacemanNethersReply struct {
	Player string  `json:"player"`
	Count  int     `json:"count"`
	Avg    string  `json:"avg"`
	NPH    float64 `json:"nph"`
	Empty  bool    `json:"empty"`
	Error  string  `json:"error,omitempty"`
}

// PacemanLastFortReply is the answer to paceman.lastfort (sesame's
// !lastfort): the most recent run that reached a second structure (bastion or
// fortress), with each split rendered as elapsed time since the run started.
// A split the run never reached renders as "" (module renders that as an em
// dash); AgoSeconds is how long ago the run's data last updated. Empty is
// true when the lookback window holds no such run.
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

// PacemanPersonalBestReply is the answer to paceman.personal_best (sesame's
// !pb daily/weekly/monthly and the bare all-time form): the player's PaceMan
// personal best for the requested window. PaceMan precomputes all four
// windows on the same /user lookup (see fetchUserPBs), so this is always one
// upstream call no matter which window was asked for. Window echoes the
// normalized window the reply answers ("daily", "weekly", "monthly" or
// "all-time"), for a template that wants to say which one it printed. Empty
// is true when the player has not set a personal best in that window yet —
// a normal PaceMan answer, not an error — and Time is "" in that case.
type PacemanPersonalBestReply struct {
	Player string `json:"player"`
	Window string `json:"window"`
	Time   string `json:"time"`
	Empty  bool   `json:"empty"`
	Error  string `json:"error,omitempty"`
}

// --- mcsr: match history, versus, leaderboards, weekly race -----------------
//
// These four extend the mcsr provider beyond !elo/!session. Every upstream
// call they back is a single request (no paging loop, no client-side
// aggregation over match history): mcsr.last_match asks for one match,
// mcsr.versus reads the upstream's own precomputed head-to-head totals,
// mcsr.leaderboard takes whatever one board's full array the upstream
// returns and keeps the top few, and mcsr.weekly_race scans the one
// leaderboard slice the upstream already returns for the queried player
// instead of asking the upstream to filter it.

// McsrLastMatchReply is the answer to mcsr.last_match (sesame's !lastmatch):
// the player's most recent match. Time is the match's completion time
// ("m:ss.mmm"), which belongs to whoever's Result the match reports —
// it is the run's ending time, not "your" time specifically; empty when
// the match has no completion (a draw, or a forfeit called before either
// side finished). Forfeited/Decayed are surfaced separately from Result so a
// forfeit or a decay-protection match never gets rendered as an ordinary
// race — see the module's mcsrMatchResultText. EloChange is 0 for a player
// who was unrated going into the match, same nullable-in-upstream case as
// McsrUserReply. Empty is true when the player has no matches at all yet,
// a normal answer, not an error.
type McsrLastMatchReply struct {
	Player     string `json:"player"`
	Opponent   string `json:"opponent"`
	Result     string `json:"result"` // "win" | "loss" | "draw"
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

// McsrRecordReply is the answer to mcsr.versus (sesame's !record): the
// head-to-head totals between two players, ranked and casual matches
// combined (the upstream reports each queue's total separately; "played"
// here is their sum, since the command promises one grand total). WinsA and
// WinsB are keyed to PlayerA/PlayerB by the provider (the upstream's own
// players[] array is not guaranteed to list them in request order).
type McsrRecordReply struct {
	PlayerA string `json:"player_a"`
	PlayerB string `json:"player_b"`
	WinsA   int    `json:"wins_a"`
	WinsB   int    `json:"wins_b"`
	Played  int    `json:"played"`
	Error   string `json:"error,omitempty"`
}

// McsrLeaderboardEntry is one ranked row on any of !lb's three boards: a
// display name plus whichever single number that board ranks on (elo, phase
// points, or a season-best time as "m:ss.mmm"), pre-formatted by the
// provider so the module never needs to know which board produced it.
type McsrLeaderboardEntry struct {
	Rank  int    `json:"rank"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// McsrLeaderboardReply is the answer to mcsr.leaderboard (sesame's !lb): the
// top of whichever board Board selected. This is the gossip surface's first
// list-shaped reply — every other reply here is flat scalar fields because
// each one describes a single player or a single match. A leaderboard is
// structurally different: chat wants a handful of ranked (name, value) pairs
// together, not five independently-named fields (Name1..Name5/Value1..Value5)
// that would have to grow every time "top 5" changed to "top 10" and would
// not let the module tell "no 5th place exists" from "5th place is blank".
// A slice carries its own length, so Entries shorter than 5 (a thin
// leaderboard, a narrow country filter) is exactly its own answer; Empty
// covers the zero-length case explicitly since "nobody on this board yet" is
// a normal reply, not an error.
type McsrLeaderboardReply struct {
	Board   string                 `json:"board"`
	Entries []McsrLeaderboardEntry `json:"entries"`
	Empty   bool                   `json:"empty"`
	Error   string                 `json:"error,omitempty"`
}

// McsrWeeklyRaceReply is the answer to mcsr.weekly_race (sesame's !race):
// the current weekly-race seed's #1 holder, plus the queried player's own
// time and rank when they appear on the same leaderboard slice the upstream
// already returned (the API has no per-player filter for this endpoint, so
// the provider scans the one response instead of calling again). HasPlayer
// false is a normal answer (they have not submitted a time this week yet),
// same as Empty being true for nobody having submitted one at all.
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

// --- clashroyale (Supercell Clash Royale via RoyaleAPI's proxy) --------------
//
// All four endpoints derive from the same GET /players/{tag} profile, so their
// replies are projections of one cached payload; nested shapes below mirror the
// upstream's own JSON keys where a chat template wants them verbatim.

// ClashRoyaleArena is the arena a player currently sits in.
type ClashRoyaleArena struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ClashRoyaleClan is the clan a player belongs to; zero-valued when clanless.
type ClashRoyaleClan struct {
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	BadgeID int64  `json:"badgeId,omitempty"`
}

// ClashRoyaleCard is one card as it appears in a deck or favourite slot: the
// bot-needed subset of the upstream card object, icon URLs included.
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

// ClashRoyaleRankedResult covers both Path of Legends results and the legacy
// league season records. Fields absent in one representation remain
// zero-valued.
type ClashRoyaleRankedResult struct {
	SeasonID     string `json:"id,omitempty"`
	LeagueNumber int    `json:"leagueNumber,omitempty"`
	Trophies     int    `json:"trophies,omitempty"`
	BestTrophies int    `json:"bestTrophies,omitempty"`
	Rank         int    `json:"rank,omitempty"`
}

// ClashRoyaleStatsReply is the answer to clashroyale.stats (sesame's !crstats):
// a player's lifetime profile. Draws is derived (battles minus wins and
// losses) and WinRate is a percentage over battles.
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

// ClashRoyaleDecksReply is the answer to clashroyale.decks (sesame's !crdecks):
// the player's current battle deck and tower troop, with the elixir average
// precomputed to two decimals.
type ClashRoyaleDecksReply struct {
	Player        string            `json:"player"`
	Tag           string            `json:"tag"`
	CurrentDeck   []ClashRoyaleCard `json:"current_deck"`
	SupportCards  []ClashRoyaleCard `json:"support_cards"`
	AverageElixir float64           `json:"average_elixir"`
	Error         string            `json:"error,omitempty"`
}

// ClashRoyaleRankedReply is the answer to clashroyale.ranked (sesame's
// !crranked): the player's Path of Legends standing, falling back to the
// legacy league seasons for players without PoL records. Unranked is true when
// neither source has a current result.
type ClashRoyaleRankedReply struct {
	Player   string                  `json:"player"`
	Tag      string                  `json:"tag"`
	Current  ClashRoyaleRankedResult `json:"current"`
	Previous ClashRoyaleRankedResult `json:"previous"`
	Best     ClashRoyaleRankedResult `json:"best"`
	Unranked bool                    `json:"unranked"`
	Error    string                  `json:"error,omitempty"`
}

// ClashRoyaleTrophyRoadReply is the answer to clashroyale.trophy_road
// (sesame's !crtrophy): the player's trophy-road standing — current and best
// trophies plus the arena they sit in.
type ClashRoyaleTrophyRoadReply struct {
	Player       string           `json:"player"`
	Tag          string           `json:"tag"`
	Trophies     int              `json:"trophies"`
	BestTrophies int              `json:"best_trophies"`
	Arena        ClashRoyaleArena `json:"arena"`
	Error        string           `json:"error,omitempty"`
}

// --- valorant (HenrikDev community API lookups) -----------------------------

// ValorantRankReply is the answer to valorant.rank (sesame's !valrank): the
// account's current competitive standing. LastChange is the RR delta of the
// most recent competitive game (negative on a loss); Placement is the current
// act leaderboard position, 0 when unplaced. Unranked is true for accounts
// without a act placement — Elo is zeroed alongside it so templates never
// print "-23 elo, Unranked".
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

// ValorantMatchEntry summarizes one completed competitive game for a chat
// line. ACS is precomputed upstream (score over team rounds, one decimal) and
// AgoSeconds lets a template say how old the game is.
type ValorantMatchEntry struct {
	Map        string  `json:"map"`
	Agent      string  `json:"agent"`
	Result     string  `json:"result"` // "win" | "loss" | "draw"
	Kills      int     `json:"kills"`
	Deaths     int     `json:"deaths"`
	Assists    int     `json:"assists"`
	ACS        float64 `json:"acs"`
	AgoSeconds int64   `json:"ago_seconds"`
}

// ValorantMatchesReply is the answer to valorant.matches (sesame's
// !valmatches): the last few completed competitive games, newest first.
// Incomplete games are skipped rather than shown as ghost rows. Empty is true
// when the account simply has no ranked games in the retained window — a
// normal answer, not an error.
type ValorantMatchesReply struct {
	Player  string               `json:"player"`
	Region  string               `json:"region"`
	Matches []ValorantMatchEntry `json:"matches"`
	Empty   bool                 `json:"empty"`
	Error   string               `json:"error,omitempty"`
}

// ValorantAccountReply is the answer to valorant.account (sesame's
// !valaccount): who a Riot ID resolves to, plus the level/card/title flex. It
// doubles as the cheapest correctness probe for a caller unsure of spelling or
// region.
type ValorantAccountReply struct {
	Player       string `json:"player"`
	Puuid        string `json:"puuid,omitempty"`
	Region       string `json:"region,omitempty"`
	AccountLevel int    `json:"account_level,omitempty"`
	Card         string `json:"card,omitempty"`
	Title        string `json:"title,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ValorantLeaderboardEntry is one ranked row of a regional board. Tier stays
// the numeric competitive tier id (3 = Iron 1 up to 27 = Radiant) rather than a
// spelled-out name, matching what every tracker renders from.
type ValorantLeaderboardEntry struct {
	Rank   int    `json:"rank"`
	Player string `json:"player"`
	Tier   int    `json:"tier"`
	RR     int    `json:"rr"`
	Wins   int    `json:"wins"`
}

// ValorantLeaderboardReply is the answer to valorant.leaderboard (sesame's
// !vallb): the top slice of one regional board. Board echoes
// "<region>/<platform>" so a template can say which board it printed; Player
// echoes the account scoping it when one was given ("" for a bare top-N ask).
type ValorantLeaderboardReply struct {
	Player  string                     `json:"player,omitempty"`
	Board   string                     `json:"board,omitempty"`
	Entries []ValorantLeaderboardEntry `json:"entries"`
	Empty   bool                       `json:"empty"`
	Error   string                     `json:"error,omitempty"`
}

// ValorantShopItem is one rotated skin ready for rendering. Color is the
// rarity's background hex straight from Riot's content CDN, so a renderer can
// colour-code without owning a rarity table.
type ValorantShopItem struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
	Tier  string `json:"tier,omitempty"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// ValorantShopReply is the answer to valorant.shop (sesame's !valshop):
// today's global skin rotation, priciest first. ResetUnix is the instant the
// rotation turns over (03:00 UTC), so a template can print a countdown. Empty
// is true on the rare days Riot rotates nothing — a normal answer, not an
// error.
type ValorantShopReply struct {
	ResetUnix int64              `json:"reset_unix"`
	Items     []ValorantShopItem `json:"items"`
	Count     int                `json:"count"`
	Empty     bool               `json:"empty"`
	Error     string             `json:"error,omitempty"`
}
