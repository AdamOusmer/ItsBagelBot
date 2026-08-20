// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package mcsr is the gossip provider for the MCSR Ranked public API: a
// player's current ranked standing, plus the per-channel stream-session delta
// sesame's !session command shows.
//
// The session flow is snapshot-based: when a stream goes online sesame calls
// session_start, which stores the player's standing under the broadcaster's
// channel id. A later session call diffs the live standing against that
// snapshot, so "this stream" means exactly the live session — the value the
// dashboard module page promises.
package mcsr

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	// userTTL keeps chat spam off the MCSR API (500 req / 10 min fleet-wide).
	// Sized against the game, not against a round number: the world record sits
	// around 6:35, so no run can start and finish inside this window, and a
	// viewer asking twice about the same player is asking about the same run.
	// At one minute the entry expired faster than the upstream answered on a
	// cold call, so essentially every !mcsr paid a full round trip to an API
	// that returns the same numbers.
	userTTL = 6 * time.Minute
	// snapshotTTL outlives any plausible single stream; Twitch caps broadcasts
	// at 48h.
	snapshotTTL = 49 * time.Hour

	// lastMatchTTL: a finished match is a discrete event, not a slowly moving
	// stat block, so a viewer asking "what just happened" wants it fresher
	// than userTTL's season aggregate. Half of userTTL keeps a second !lastmatch
	// from a chat pile-on off the API without going so short that a match
	// finishing mid-window costs its own round trip anyway.
	lastMatchTTL = 3 * time.Minute
	// recordTTL: the head-to-head total between two specific players only
	// moves when those two specific players finish a match against each
	// other, a far rarer event than either one finishing any match, so it can
	// sit longer than lastMatchTTL without going stale in practice.
	recordTTL = 10 * time.Minute
	// leaderboardTTL covers the elo and phase-point boards: both are full,
	// unbounded arrays (see fetchEloLeaderboard/fetchPhaseLeaderboard), the
	// heaviest single payload this provider pulls, and the ranking only
	// reshuffles at the pace of completed ranked matches across the entire
	// player base, not per viewer request.
	leaderboardTTL = 5 * time.Minute
	// recordLeaderboardTTL: season-best times move even less often than elo —
	// only when someone actually beats a existing personal best on that
	// seed pool — so it can sit longer than the rating boards.
	recordLeaderboardTTL = 10 * time.Minute
	// raceTTL: the weekly race leaderboard updates whenever any entrant
	// submits a faster run against the shared seed, a similar cadence to a
	// single player's match history, so it reuses lastMatchTTL's reasoning.
	raceTTL = lastMatchTTL
	// negativeTTL is shared by every mcsr.* cache below (cachedUser inlines
	// the same 5-minute value; a "not found" answer is worth remembering
	// almost as long as a real one, since retrying it costs a full round trip
	// for the same negative result).
	negativeTTL = 5 * time.Minute

	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second
)

// Config carries the provider's environment. APIKey is optional: MCSR grants
// expanded rate limits to keyed clients via the Private-Key header.
type Config struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
}

// providerName is the subject token this provider answers under.
const providerName = "mcsr"

// rateWindowSeconds is the MCSR budget window (10 minutes: 500 requests).
const rateWindowSeconds = 600.0

// api holds the provider's runtime pieces; the declared endpoints capture it.
// The endpoints stay bespoke handlers (not byte-flows): they answer typed
// replies whose snapshot side effects and elo semantics do not fit the shared
// cached-bytes skeleton.
type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	log     *zap.Logger
	limiter *ratelimit.Limiter
	buckets core.Buckets
}

// New builds the mcsr provider.
func New(cfg Config, d provider.Deps) provider.Provider {
	p := newAPI(cfg, d)
	b := provider.NewProvider(providerName, d)
	b.Endpoint("user").Timeout(handlerTimeout).Handle(p.user)
	b.Endpoint("session_start").Timeout(handlerTimeout).Handle(p.sessionStart)
	b.Endpoint("session").Timeout(handlerTimeout).Handle(p.session)
	b.Endpoint("last_match").Timeout(handlerTimeout).Handle(p.lastMatch)
	b.Endpoint("versus").Timeout(handlerTimeout).Handle(p.versus)
	b.Endpoint("leaderboard").Timeout(handlerTimeout).Handle(p.leaderboard)
	b.Endpoint("weekly_race").Timeout(handlerTimeout).Handle(p.weeklyRace)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.mcsrranked.com"
	}
	var headers map[string]string
	if cfg.APIKey != "" {
		headers = map[string]string{"Private-Key": cfg.APIKey}
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 500
	}
	return &api{
		http:    core.NewHTTPClient(base, headers, httpTimeout),
		cache:   d.Cache,
		log:     d.Logger(),
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:mcsr", cfg.RateLimit, rateWindowSeconds),
	}
}

// --- upstream shapes -----------------------------------------------------------

// userResponse is the /users/{identifier} envelope subset gossip reads.
// eloRate/eloRank are null for an unrated player. statistics.season maps a
// category name to per-queue counters; the ranked queue is the one MCSR Ranked
// is about.
type userResponse struct {
	Status string `json:"status"`
	Data   struct {
		UUID       string `json:"uuid"`
		Nickname   string `json:"nickname"`
		EloRate    *int   `json:"eloRate"`
		EloRank    *int   `json:"eloRank"`
		Country    string `json:"country"`
		Statistics struct {
			Season map[string]struct {
				Ranked *int64 `json:"ranked"`
			} `json:"season"`
		} `json:"statistics"`
	} `json:"data"`
}

// snapshot is the stream-start standing stored per channel.
type snapshot struct {
	Account  string `json:"account"`
	Nickname string `json:"nickname"`
	Elo      int    `json:"elo"`
	Wins     int    `json:"wins"`
	Loses    int    `json:"loses"`
	Played   int    `json:"played"`
	AtUnix   int64  `json:"at_unix"`
}

func snapshotKey(channelID string) string { return core.Key("mcsr", "session", channelID) }

// friendlyError maps an upstream failure onto a user-facing reply error, or
// returns "" for an infrastructure failure. The MCSR API answers 400 for "data
// not found" and 401 for wrong parameters.
func friendlyError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		switch ue.Status {
		case 400, 401, 404:
			return "player not found"
		case 429:
			return "MCSR Ranked API is busy, try again in a minute"
		}
	}
	return ""
}

// enforceRateLimit consumes one request from the MCSR budget under the shared
// premium/standard bucket discipline (see core.Buckets).
func (p *api) enforceRateLimit(ctx context.Context, isPremium bool) error {
	return p.buckets.Enforce(ctx, p.limiter, isPremium)
}

// admit binds the budget to one lane for a cached lookup. It is handed to
// core.Cached rather than written inside the fill because a fill runs once per
// singleflight flight: a check in there is charged to whichever caller won the
// flight and its verdict is served to everyone joined to it, so a drained
// standard bucket would deny premium callers the reserve they are entitled to.
func (p *api) admit(isPremium bool) func(context.Context) error {
	return func(ctx context.Context) error { return p.enforceRateLimit(ctx, isPremium) }
}

// fetchUser loads a player's live standing straight from the API.
func (p *api) fetchUser(ctx context.Context, account string) (gossiprpc.McsrUserReply, error) {
	var resp userResponse
	if err := p.http.GetJSON(ctx, "/users/"+strings.TrimSpace(account), nil, &resp); err != nil {
		return gossiprpc.McsrUserReply{}, err
	}
	d := resp.Data

	season := func(cat string) int {
		if s, ok := d.Statistics.Season[cat]; ok && s.Ranked != nil {
			return int(*s.Ranked)
		}
		return 0
	}
	bestTime := int64(0)
	if s, ok := d.Statistics.Season["bestTime"]; ok && s.Ranked != nil {
		bestTime = *s.Ranked
	}

	reply := gossiprpc.McsrUserReply{
		Nickname:   d.Nickname,
		UUID:       d.UUID,
		Elo:        -1,
		Rank:       -1,
		Country:    d.Country,
		Wins:       season("wins"),
		Loses:      season("loses"),
		Played:     season("playedMatches"),
		BestTimeMS: bestTime,
	}
	if d.EloRate != nil {
		reply.Elo = *d.EloRate
	}
	if d.EloRank != nil {
		reply.Rank = *d.EloRank
	}
	return reply, nil
}

// cachedUser is fetchUser behind the shared 60s cache.
func (p *api) cachedUser(ctx context.Context, account string, isPremium bool) (gossiprpc.McsrUserReply, error) {
	key := core.Key(providerName, "user", strings.ToLower(strings.TrimSpace(account)))
	return core.Cached(ctx, p.cache, key, userTTL, 5*time.Minute, p.admit(isPremium), func(ctx context.Context) (gossiprpc.McsrUserReply, error) {
		return p.fetchUser(ctx, account)
	})
}

// --- endpoints ------------------------------------------------------------------

func (p *api) user(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.McsrUserReply{Error: "missing account"}
	}
	reply, err := p.cachedUser(ctx, account, req.IsPremium)
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gossiprpc.McsrUserReply{Nickname: account, Error: msg}
		}
		log.Warn("mcsr user fetch failed", zap.String("account", account), zap.Error(err))
		return gossiprpc.McsrUserReply{Nickname: account, Error: "stats lookup failed"}
	}
	return reply
}

// sessionStart snapshots the player's live standing for the channel. It
// fetches fresh (not through the 60s cache): the snapshot is the session
// baseline, so it must not predate the stream by a stale cache window.
func (p *api) sessionStart(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gossiprpc.McsrSnapshotReply{Error: "missing account or channel"}
	}
	// Spent inline: this path deliberately bypasses the cache (the snapshot is the
	// session baseline and must not predate the stream), so no cached admission
	// carries the debit for it.
	if err := p.enforceRateLimit(ctx, req.IsPremium); err != nil {
		return gossiprpc.McsrSnapshotReply{Error: friendlyError(err)}
	}
	user, err := p.fetchUser(ctx, account)
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gossiprpc.McsrSnapshotReply{Error: msg}
		}
		log.Warn("mcsr snapshot fetch failed", zap.String("account", account), zap.Error(err))
		return gossiprpc.McsrSnapshotReply{Error: "stats lookup failed"}
	}
	if err := p.writeSnapshot(ctx, req.ChannelID, account, user); err != nil {
		log.Warn("mcsr snapshot write failed", zap.String("channel_id", req.ChannelID), zap.Error(err))
		return gossiprpc.McsrSnapshotReply{Error: "snapshot store failed"}
	}
	return gossiprpc.McsrSnapshotReply{Nickname: user.Nickname, Elo: user.Elo}
}

func (p *api) writeSnapshot(ctx context.Context, channelID, account string, user gossiprpc.McsrUserReply) error {
	return p.cache.SetJSON(ctx, snapshotKey(channelID), snapshot{
		Account:  strings.ToLower(account),
		Nickname: user.Nickname,
		Elo:      user.Elo,
		Wins:     user.Wins,
		Loses:    user.Loses,
		Played:   user.Played,
		AtUnix:   time.Now().Unix(),
	}, snapshotTTL)
}

// session answers the delta since the channel's stream-start snapshot. Without
// a usable snapshot (none stored, or it tracks a different account) it takes
// one now and reports HasSnapshot=false so the caller can say "tracking from
// now". Split into sessionUser/sessionSnapshot/startSession/sessionDelta below
// (each a self-contained step: fetch, read, take-fresh, build) rather than one
// function carrying every branch — the "Bumpy Road" shape CodeScene flags when
// several independent conditionals sit at the same nesting level.
// sessionRequest bundles !session's per-call values threaded through
// sessionUser/sessionSnapshot/startSession: the txn-scoped logger, channel
// and account. Passing one value keeps each step's own signature short
// instead of growing by one parameter every time the flow needs another.
type sessionRequest struct {
	log       *zap.Logger
	channelID string
	account   string
	isPremium bool
}

func (p *api) session(ctx context.Context, req gossiprpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gossiprpc.McsrSessionReply{Error: "missing account or channel"}
	}
	sreq := sessionRequest{
		log:       monitor.TxnLogger(ctx, p.log),
		channelID: req.ChannelID,
		account:   account,
		isPremium: req.IsPremium,
	}

	user, errReply, ok := p.sessionUser(ctx, sreq)
	if !ok {
		return errReply
	}

	snap, hasSnapshot := p.sessionSnapshot(ctx, sreq)
	if !hasSnapshot {
		return p.startSession(ctx, sreq, user)
	}
	return sessionDelta(user, snap)
}

// sessionUser fetches !session's account standing, turning an upstream
// failure into the reply's Error field (ok=false) instead of propagating it —
// the same "always answer something" contract every endpoint in this file
// follows.
func (p *api) sessionUser(ctx context.Context, req sessionRequest) (user gossiprpc.McsrUserReply, errReply gossiprpc.McsrSessionReply, ok bool) {
	user, err := p.cachedUser(ctx, req.account, req.isPremium)
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gossiprpc.McsrUserReply{}, gossiprpc.McsrSessionReply{Nickname: req.account, Error: msg}, false
		}
		req.log.Warn("mcsr session fetch failed", zap.String("account", req.account), zap.Error(err))
		return gossiprpc.McsrUserReply{}, gossiprpc.McsrSessionReply{Nickname: req.account, Error: "stats lookup failed"}, false
	}
	return user, gossiprpc.McsrSessionReply{}, true
}

// sessionSnapshot reads the channel's stream-start snapshot. hasSnapshot is
// false both when none is stored and when it tracks a different account (a
// linked-account change mid-stream starts a fresh baseline instead of
// diffing against someone else's numbers).
func (p *api) sessionSnapshot(ctx context.Context, req sessionRequest) (snap snapshot, hasSnapshot bool) {
	ok, err := p.cache.GetJSON(ctx, snapshotKey(req.channelID), &snap)
	if err != nil {
		req.log.Warn("mcsr snapshot read failed", zap.String("channel_id", req.channelID), zap.Error(err))
	}
	if !ok || snap.Account != strings.ToLower(req.account) {
		return snapshot{}, false
	}
	return snap, true
}

// startSession takes a fresh stream-start snapshot and answers
// HasSnapshot=false so the caller can say "tracking from now" instead of a
// fake zero delta.
func (p *api) startSession(ctx context.Context, req sessionRequest, user gossiprpc.McsrUserReply) gossiprpc.McsrSessionReply {
	if err := p.writeSnapshot(ctx, req.channelID, req.account, user); err != nil {
		req.log.Warn("mcsr snapshot write failed", zap.String("channel_id", req.channelID), zap.Error(err))
	}
	return gossiprpc.McsrSessionReply{
		Nickname:    user.Nickname,
		Elo:         user.Elo,
		HasSnapshot: false,
		SinceUnix:   time.Now().Unix(),
	}
}

// sessionDelta builds the !session reply from the account's current standing
// and the channel's stream-start snapshot.
func sessionDelta(user gossiprpc.McsrUserReply, snap snapshot) gossiprpc.McsrSessionReply {
	reply := gossiprpc.McsrSessionReply{
		Nickname:    user.Nickname,
		Elo:         user.Elo,
		Wins:        user.Wins - snap.Wins,
		Loses:       user.Loses - snap.Loses,
		Played:      user.Played - snap.Played,
		SinceUnix:   snap.AtUnix,
		HasSnapshot: true,
	}
	// Elo change only means something when both ends are rated.
	if user.Elo >= 0 && snap.Elo >= 0 {
		reply.EloChange = user.Elo - snap.Elo
	}
	return reply
}

// --- upstream shapes: match history / versus --------------------------------

// matchPlayer is one entry in a matchInfo/versus players[] array.
type matchPlayer struct {
	UUID     string `json:"uuid"`
	Nickname string `json:"nickname"`
}

// matchChange is one player's elo delta from a matchInfo.changes[] entry.
// Change is nil for a player who was unrated going into the match.
type matchChange struct {
	UUID   string `json:"uuid"`
	Change *int   `json:"change"`
}

// matchResultRef is a matchInfo's winner pointer: UUID nil means a draw or no
// result, otherwise it names the winning player and Time is their completion
// time in milliseconds.
type matchResultRef struct {
	UUID *string `json:"uuid"`
	Time int64   `json:"time"`
}

// matchInfo is the MatchInfo shape shared by /users/{id}/matches and
// /matches/{id}: one match, completed, forfeited, or decayed.
type matchInfo struct {
	Date        int64          `json:"date"`
	SeedType    *string        `json:"seedType"`
	BastionType *string        `json:"bastionType"`
	Forfeited   bool           `json:"forfeited"`
	Decayed     bool           `json:"decayed"`
	Players     []matchPlayer  `json:"players"`
	Result      matchResultRef `json:"result"`
	Changes     []matchChange  `json:"changes"`
}

type matchesResponse struct {
	Status string      `json:"status"`
	Data   []matchInfo `json:"data"`
}

// versusResponse is /users/{a}/versus/{b}'s envelope. Ranked/Casual key
// player uuids to that queue's win count, plus a fixed "total" key for the
// queue's match count; !record sums both queues for its grand total.
type versusResponse struct {
	Status string `json:"status"`
	Data   struct {
		Players []matchPlayer `json:"players"`
		Results struct {
			Ranked map[string]int64 `json:"ranked"`
			Casual map[string]int64 `json:"casual"`
		} `json:"results"`
	} `json:"data"`
}

// --- upstream shapes: leaderboards ------------------------------------------

// lbUser is one row shared by the elo and phase-point leaderboards: a
// UserProfile plus the one season-scoped number each board ranks on.
type lbUser struct {
	Nickname     string `json:"nickname"`
	SeasonResult struct {
		EloRate        *int `json:"eloRate"`
		PhasePoint     *int `json:"phasePoint"`
		PredPhasePoint *int `json:"predPhasePoint"`
	} `json:"seasonResult"`
}

type usersLeaderboardResponse struct {
	Status string `json:"status"`
	Data   struct {
		Users []lbUser `json:"users"`
	} `json:"data"`
}

// recordEntry is one row of /record-leaderboard: flat, not nested under a
// "users" key like the other two boards.
type recordEntry struct {
	Rank int   `json:"rank"`
	Time int64 `json:"time"`
	User struct {
		Nickname string `json:"nickname"`
	} `json:"user"`
}

type recordLeaderboardResponse struct {
	Status string        `json:"status"`
	Data   []recordEntry `json:"data"`
}

// --- upstream shapes: weekly race -------------------------------------------

type weeklyRaceEntry struct {
	Rank   int `json:"rank"`
	Player struct {
		Nickname string `json:"nickname"`
	} `json:"player"`
	Time int64 `json:"time"`
}

type weeklyRaceResponse struct {
	Status string `json:"status"`
	Data   struct {
		ID          int               `json:"id"`
		Leaderboard []weeklyRaceEntry `json:"leaderboard"`
	} `json:"data"`
}

// --- shared helpers ----------------------------------------------------------

// leaderboardLimit is !lb's "top 5" — chat gets one line, not the full
// (sometimes unbounded) board the upstream returns.
const leaderboardLimit = 5

// mcsrFormatTime renders a completion time in milliseconds the way MCSR
// Ranked's own clients display a run: minutes:seconds.milliseconds. A
// non-positive input (no completion — a draw, or a forfeit called before
// anyone finished) renders as "" so the module can dash it like any other
// missing split.
func mcsrFormatTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	millis := ms % 1000
	return fmt.Sprintf("%d:%02d.%03d", minutes, seconds, millis)
}

// mcsrTitleCase renders an upstream SCREAMING_SNAKE_CASE enum
// ("DESERT_TEMPLE") as words ("Desert Temple") for chat display.
func mcsrTitleCase(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

// mcsrCacheID folds an account and season into one cache-key id so two
// different seasons of the same lookup never collide on one entry; "0"
// stands for "current season" the same way an unset Season does on the wire.
func mcsrCacheID(account string, season int) string {
	return strings.ToLower(strings.TrimSpace(account)) + ":" + strconv.Itoa(season)
}

// matchSelf splits a match's two players into (self, opponent). It matches
// by nickname (the common case: a viewer types a Minecraft username) then by
// dashless uuid; when neither typed identifier is recognized (a discord.<id>
// identifier, which the players[] array carries no field for) it falls back
// to positional order rather than failing the whole command — a second
// upstream call to resolve it is not on the table for a one-call command.
// Only the first two entries are read (1v1 assumed, per the spec's own note
// that an FFA match is shaped differently).
func matchSelf(account string, players []matchPlayer) (self, opponent matchPlayer, ok bool) {
	if len(players) < 2 {
		return matchPlayer{}, matchPlayer{}, false
	}
	a, b := players[0], players[1]
	if matchPlayerIs(account, b) {
		return b, a, true
	}
	return a, b, true
}

func matchPlayerIs(account string, p matchPlayer) bool {
	needle := strings.ToLower(strings.TrimSpace(account))
	if strings.ToLower(p.Nickname) == needle {
		return true
	}
	return strings.EqualFold(strings.ReplaceAll(p.UUID, "-", ""), strings.ReplaceAll(needle, "-", ""))
}

// matchResult reports a match's outcome from selfUUID's perspective.
func matchResult(m matchInfo, selfUUID string) string {
	switch {
	case m.Result.UUID == nil:
		return "draw"
	case *m.Result.UUID == selfUUID:
		return "win"
	default:
		return "loss"
	}
}

// matchEloChange finds selfUUID's elo delta among a match's changes[],
// answering 0 for a player who was unrated going in (Change is nil then).
func matchEloChange(changes []matchChange, selfUUID string) int {
	for _, c := range changes {
		if c.UUID == selfUUID && c.Change != nil {
			return *c.Change
		}
	}
	return 0
}

// --- fetch: match history / versus ------------------------------------------

func (p *api) fetchLastMatch(ctx context.Context, account string, season int) (matchesResponse, error) {
	var resp matchesResponse
	q := url.Values{"count": {"1"}}
	if season > 0 {
		q.Set("season", strconv.Itoa(season))
	}
	if err := p.http.GetJSON(ctx, "/users/"+strings.TrimSpace(account)+"/matches", q, &resp); err != nil {
		return matchesResponse{}, err
	}
	return resp, nil
}

func (p *api) cachedLastMatch(ctx context.Context, account string, season int, isPremium bool) (matchesResponse, error) {
	key := core.Key(providerName, "last-match", mcsrCacheID(account, season))
	return core.Cached(ctx, p.cache, key, lastMatchTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (matchesResponse, error) {
		return p.fetchLastMatch(ctx, account, season)
	})
}

// versusQuery bundles !record's two accounts and season so fetchVersus and
// cachedVersus take one named value instead of three loose parameters
// alongside ctx (and isPremium, for the cached variant).
type versusQuery struct {
	A      string
	B      string
	Season int
}

func (p *api) fetchVersus(ctx context.Context, q versusQuery) (versusResponse, error) {
	var resp versusResponse
	var vals url.Values
	if q.Season > 0 {
		vals = url.Values{"season": {strconv.Itoa(q.Season)}}
	}
	path := "/users/" + strings.TrimSpace(q.A) + "/versus/" + strings.TrimSpace(q.B)
	if err := p.http.GetJSON(ctx, path, vals, &resp); err != nil {
		return versusResponse{}, err
	}
	return resp, nil
}

func (p *api) cachedVersus(ctx context.Context, q versusQuery, isPremium bool) (versusResponse, error) {
	id := mcsrCacheID(q.A, q.Season) + "|" + strings.ToLower(strings.TrimSpace(q.B))
	key := core.Key(providerName, "versus", id)
	return core.Cached(ctx, p.cache, key, recordTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (versusResponse, error) {
		return p.fetchVersus(ctx, q)
	})
}

// --- fetch: leaderboards -----------------------------------------------------

func leaderboardQuery(season int, country string) url.Values {
	q := url.Values{}
	if season > 0 {
		q.Set("season", strconv.Itoa(season))
	}
	if country != "" {
		q.Set("country", strings.ToLower(country))
	}
	return q
}

func (p *api) fetchEloLeaderboard(ctx context.Context, season int, country string) ([]lbUser, error) {
	var resp usersLeaderboardResponse
	if err := p.http.GetJSON(ctx, "/leaderboard", leaderboardQuery(season, country), &resp); err != nil {
		return nil, err
	}
	return resp.Data.Users, nil
}

// lbQuery bundles a leaderboard lookup's season/country/predicted filters so
// cachedPhaseLeaderboard and fetchPhaseLeaderboard take one named value
// instead of three loose parameters alongside ctx (and isPremium, for the
// cached variant).
type lbQuery struct {
	Season    int
	Country   string
	Predicted bool
}

func (p *api) fetchPhaseLeaderboard(ctx context.Context, q lbQuery) ([]lbUser, error) {
	var resp usersLeaderboardResponse
	vals := leaderboardQuery(q.Season, q.Country)
	if q.Predicted {
		vals.Set("predicted", "true")
	}
	if err := p.http.GetJSON(ctx, "/phase-leaderboard", vals, &resp); err != nil {
		return nil, err
	}
	return resp.Data.Users, nil
}

// fetchRecordLeaderboard always sends season explicitly (default "0"), never
// omits it: /record-leaderboard's own default for a missing param is "all
// seasons combined" (spec section 3), the one board whose "unset" behavior
// differs from the rest of mcsr's "unset means current season" convention.
// Sending "0" is what actually asks for the current season here.
func (p *api) fetchRecordLeaderboard(ctx context.Context, season int) ([]recordEntry, error) {
	var resp recordLeaderboardResponse
	q := url.Values{"season": {strconv.Itoa(season)}}
	if err := p.http.GetJSON(ctx, "/record-leaderboard", q, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func leaderboardCacheID(season int, country string, predicted bool) string {
	id := strconv.Itoa(season) + ":" + strings.ToLower(country)
	if predicted {
		id += ":predicted"
	}
	return id
}

func (p *api) cachedEloLeaderboard(ctx context.Context, season int, country string, isPremium bool) ([]lbUser, error) {
	key := core.Key(providerName, "leaderboard-elo", leaderboardCacheID(season, country, false))
	return core.Cached(ctx, p.cache, key, leaderboardTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) ([]lbUser, error) {
		return p.fetchEloLeaderboard(ctx, season, country)
	})
}

func (p *api) cachedPhaseLeaderboard(ctx context.Context, q lbQuery, isPremium bool) ([]lbUser, error) {
	key := core.Key(providerName, "leaderboard-phase", leaderboardCacheID(q.Season, q.Country, q.Predicted))
	return core.Cached(ctx, p.cache, key, leaderboardTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) ([]lbUser, error) {
		return p.fetchPhaseLeaderboard(ctx, q)
	})
}

func (p *api) cachedRecordLeaderboard(ctx context.Context, season int, isPremium bool) ([]recordEntry, error) {
	key := core.Key(providerName, "leaderboard-record", strconv.Itoa(season))
	return core.Cached(ctx, p.cache, key, recordLeaderboardTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) ([]recordEntry, error) {
		return p.fetchRecordLeaderboard(ctx, season)
	})
}

// --- fetch: weekly race -------------------------------------------------------

func (p *api) fetchWeeklyRace(ctx context.Context) (weeklyRaceResponse, error) {
	var resp weeklyRaceResponse
	if err := p.http.GetJSON(ctx, "/weekly-race", nil, &resp); err != nil {
		return weeklyRaceResponse{}, err
	}
	return resp, nil
}

// cachedWeeklyRace caches the whole current-week leaderboard under one key,
// not per requesting account: the upstream has no per-player filter for this
// endpoint, so every viewer asking about a different player this week still
// shares one cached response and one upstream call — see weeklyRace's doc.
func (p *api) cachedWeeklyRace(ctx context.Context, isPremium bool) (weeklyRaceResponse, error) {
	key := core.Key(providerName, "weekly-race", "current")
	return core.Cached(ctx, p.cache, key, raceTTL, negativeTTL, p.admit(isPremium), func(ctx context.Context) (weeklyRaceResponse, error) {
		return p.fetchWeeklyRace(ctx)
	})
}

// --- endpoints: match history / versus ---------------------------------------

func (p *api) lastMatch(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.McsrLastMatchReply{Error: "missing account"}
	}
	resp, err := p.cachedLastMatch(ctx, account, req.Season, req.IsPremium)
	if err != nil {
		msg := friendlyError(err)
		if msg == "" {
			log.Warn("mcsr last match fetch failed", zap.String("account", account), zap.Error(err))
			msg = "stats lookup failed"
		}
		return gossiprpc.McsrLastMatchReply{Player: account, Error: msg}
	}
	if len(resp.Data) == 0 {
		return gossiprpc.McsrLastMatchReply{Player: account, Empty: true}
	}
	return buildLastMatchReply(account, resp.Data[0])
}

func buildLastMatchReply(account string, m matchInfo) gossiprpc.McsrLastMatchReply {
	self, opponent, ok := matchSelf(account, m.Players)
	if !ok {
		return gossiprpc.McsrLastMatchReply{Player: account, Empty: true}
	}
	seed, structure := "", ""
	if m.SeedType != nil {
		seed = mcsrTitleCase(*m.SeedType)
	}
	if m.BastionType != nil {
		structure = mcsrTitleCase(*m.BastionType)
	}
	return gossiprpc.McsrLastMatchReply{
		Player:     self.Nickname,
		Opponent:   opponent.Nickname,
		Result:     matchResult(m, self.UUID),
		Time:       mcsrFormatTime(m.Result.Time),
		Seed:       seed,
		Structure:  structure,
		EloChange:  matchEloChange(m.Changes, self.UUID),
		AgoSeconds: int64(time.Since(time.Unix(m.Date, 0)).Seconds()),
		Forfeited:  m.Forfeited,
		Decayed:    m.Decayed,
	}
}

func (p *api) versus(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	a := strings.TrimSpace(req.Account)
	b := strings.TrimSpace(req.AccountB)
	if a == "" || b == "" {
		return gossiprpc.McsrRecordReply{Error: "missing account"}
	}
	resp, err := p.cachedVersus(ctx, versusQuery{A: a, B: b, Season: req.Season}, req.IsPremium)
	if err != nil {
		msg := friendlyError(err)
		if msg == "" {
			log.Warn("mcsr versus fetch failed", zap.String("a", a), zap.String("b", b), zap.Error(err))
			msg = "stats lookup failed"
		}
		return gossiprpc.McsrRecordReply{PlayerA: a, PlayerB: b, Error: msg}
	}
	return buildRecordReply(a, b, resp)
}

// matchVersusUUIDs maps the two typed identifiers onto the upstream's
// players[] uuids; that array is not guaranteed to list them in request
// order (the spec's own sample shows identifier2 listed first). Matches by
// nickname against b, falling back to positional order otherwise — the same
// best-effort tradeoff as matchSelf, and for the same reason (no second call
// budgeted to resolve a uuid/discord.id identifier).
func matchVersusUUIDs(a string, players []matchPlayer) (uuidA, uuidB string) {
	if len(players) < 2 {
		return "", ""
	}
	if matchPlayerIs(a, players[1]) {
		return players[1].UUID, players[0].UUID
	}
	return players[0].UUID, players[1].UUID
}

func displayName(typed string, players []matchPlayer, uuid string) string {
	for _, pl := range players {
		if pl.UUID == uuid {
			return pl.Nickname
		}
	}
	return typed
}

func buildRecordReply(a, b string, resp versusResponse) gossiprpc.McsrRecordReply {
	d := resp.Data
	uuidA, uuidB := matchVersusUUIDs(a, d.Players)
	return gossiprpc.McsrRecordReply{
		PlayerA: displayName(a, d.Players, uuidA),
		PlayerB: displayName(b, d.Players, uuidB),
		WinsA:   int(d.Results.Ranked[uuidA] + d.Results.Casual[uuidA]),
		WinsB:   int(d.Results.Ranked[uuidB] + d.Results.Casual[uuidB]),
		Played:  int(d.Results.Ranked["total"] + d.Results.Casual["total"]),
	}
}

// --- endpoints: leaderboards ---------------------------------------------------

func (p *api) leaderboard(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	switch req.Board {
	case "phase":
		return p.phaseLeaderboardReply(ctx, log, req)
	case "record":
		return p.recordLeaderboardReply(ctx, log, req)
	default:
		return p.eloLeaderboardReply(ctx, log, req)
	}
}

func (p *api) eloLeaderboardReply(ctx context.Context, log *zap.Logger, req gossiprpc.Request) any {
	users, err := p.cachedEloLeaderboard(ctx, req.Season, req.Country, req.IsPremium)
	if err != nil {
		return leaderboardErrorReply(log, "elo", err)
	}
	return gossiprpc.McsrLeaderboardReply{Board: "elo", Entries: eloEntries(users), Empty: len(users) == 0}
}

func (p *api) phaseLeaderboardReply(ctx context.Context, log *zap.Logger, req gossiprpc.Request) any {
	users, err := p.cachedPhaseLeaderboard(ctx, lbQuery{Season: req.Season, Country: req.Country, Predicted: req.Predicted}, req.IsPremium)
	if err != nil {
		return leaderboardErrorReply(log, "phase", err)
	}
	return gossiprpc.McsrLeaderboardReply{Board: "phase", Entries: phaseEntries(users, req.Predicted), Empty: len(users) == 0}
}

func (p *api) recordLeaderboardReply(ctx context.Context, log *zap.Logger, req gossiprpc.Request) any {
	entries, err := p.cachedRecordLeaderboard(ctx, req.Season, req.IsPremium)
	if err != nil {
		return leaderboardErrorReply(log, "record", err)
	}
	return gossiprpc.McsrLeaderboardReply{Board: "record", Entries: recordEntries(entries), Empty: len(entries) == 0}
}

func leaderboardErrorReply(log *zap.Logger, board string, err error) gossiprpc.McsrLeaderboardReply {
	msg := friendlyError(err)
	if msg == "" {
		log.Warn("mcsr leaderboard fetch failed", zap.String("board", board), zap.Error(err))
		msg = "stats lookup failed"
	}
	return gossiprpc.McsrLeaderboardReply{Board: board, Error: msg}
}

func eloEntries(users []lbUser) []gossiprpc.McsrLeaderboardEntry {
	out := make([]gossiprpc.McsrLeaderboardEntry, 0, leaderboardLimit)
	for i, u := range users {
		if i >= leaderboardLimit {
			break
		}
		elo := 0
		if u.SeasonResult.EloRate != nil {
			elo = *u.SeasonResult.EloRate
		}
		out = append(out, gossiprpc.McsrLeaderboardEntry{Rank: i + 1, Name: u.Nickname, Value: strconv.Itoa(elo)})
	}
	return out
}

func phaseEntries(users []lbUser, predicted bool) []gossiprpc.McsrLeaderboardEntry {
	out := make([]gossiprpc.McsrLeaderboardEntry, 0, leaderboardLimit)
	for i, u := range users {
		if i >= leaderboardLimit {
			break
		}
		out = append(out, gossiprpc.McsrLeaderboardEntry{Rank: i + 1, Name: u.Nickname, Value: strconv.Itoa(phasePoint(u, predicted))})
	}
	return out
}

func phasePoint(u lbUser, predicted bool) int {
	if predicted && u.SeasonResult.PredPhasePoint != nil {
		return *u.SeasonResult.PredPhasePoint
	}
	if u.SeasonResult.PhasePoint != nil {
		return *u.SeasonResult.PhasePoint
	}
	return 0
}

func recordEntries(entries []recordEntry) []gossiprpc.McsrLeaderboardEntry {
	out := make([]gossiprpc.McsrLeaderboardEntry, 0, leaderboardLimit)
	for i, e := range entries {
		if i >= leaderboardLimit {
			break
		}
		out = append(out, gossiprpc.McsrLeaderboardEntry{Rank: e.Rank, Name: e.User.Nickname, Value: mcsrFormatTime(e.Time)})
	}
	return out
}

// --- endpoints: weekly race ------------------------------------------------

func (p *api) weeklyRace(ctx context.Context, req gossiprpc.Request) any {
	log := monitor.TxnLogger(ctx, p.log)
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gossiprpc.McsrWeeklyRaceReply{Error: "missing account"}
	}
	resp, err := p.cachedWeeklyRace(ctx, req.IsPremium)
	if err != nil {
		msg := friendlyError(err)
		if msg == "" {
			log.Warn("mcsr weekly race fetch failed", zap.Error(err))
			msg = "stats lookup failed"
		}
		return gossiprpc.McsrWeeklyRaceReply{Player: account, Error: msg}
	}
	return buildWeeklyRaceReply(account, resp.Data.Leaderboard)
}

func buildWeeklyRaceReply(account string, board []weeklyRaceEntry) gossiprpc.McsrWeeklyRaceReply {
	if len(board) == 0 {
		return gossiprpc.McsrWeeklyRaceReply{Player: account, Empty: true}
	}
	reply := gossiprpc.McsrWeeklyRaceReply{
		Player:     account,
		LeaderName: board[0].Player.Nickname,
		LeaderTime: mcsrFormatTime(board[0].Time),
	}
	needle := strings.ToLower(account)
	for _, e := range board {
		if strings.ToLower(e.Player.Nickname) == needle {
			reply.PlayerTime = mcsrFormatTime(e.Time)
			reply.PlayerRank = e.Rank
			reply.HasPlayer = true
			break
		}
	}
	return reply
}
