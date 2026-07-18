// Package mcsr is the gateway provider for the MCSR Ranked public API: a
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
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	// userTTL keeps chat spam off the MCSR API (500 req / 10 min fleet-wide)
	// while staying fresh enough that a finished match shows within a minute.
	userTTL = time.Minute
	// snapshotTTL outlives any plausible single stream; Twitch caps broadcasts
	// at 48h.
	snapshotTTL = 49 * time.Hour

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
		buckets: core.NewBuckets("ratelimit:gateway:mcsr", cfg.RateLimit, rateWindowSeconds),
	}
}

// --- upstream shapes -----------------------------------------------------------

// userResponse is the /users/{identifier} envelope subset the gateway reads.
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

// fetchUser loads a player's live standing straight from the API.
func (p *api) fetchUser(ctx context.Context, account string, isPremium bool) (gatewayrpc.McsrUserReply, error) {
	if err := p.enforceRateLimit(ctx, isPremium); err != nil {
		return gatewayrpc.McsrUserReply{}, err
	}
	var resp userResponse
	if err := p.http.GetJSON(ctx, "/users/"+strings.TrimSpace(account), nil, &resp); err != nil {
		return gatewayrpc.McsrUserReply{}, err
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

	reply := gatewayrpc.McsrUserReply{
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
func (p *api) cachedUser(ctx context.Context, account string, isPremium bool) (gatewayrpc.McsrUserReply, error) {
	key := core.Key(providerName, "user", strings.ToLower(strings.TrimSpace(account)))
	return core.Cached(ctx, p.cache, key, userTTL, 5*time.Minute, func(ctx context.Context) (gatewayrpc.McsrUserReply, error) {
		return p.fetchUser(ctx, account, isPremium)
	})
}

// --- endpoints ------------------------------------------------------------------

func (p *api) user(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.McsrUserReply{Error: "missing account"}
	}
	reply, err := p.cachedUser(ctx, account, req.IsPremium)
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gatewayrpc.McsrUserReply{Nickname: account, Error: msg}
		}
		p.log.Warn("mcsr user fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.McsrUserReply{Nickname: account, Error: "stats lookup failed"}
	}
	return reply
}

// sessionStart snapshots the player's live standing for the channel. It
// fetches fresh (not through the 60s cache): the snapshot is the session
// baseline, so it must not predate the stream by a stale cache window.
func (p *api) sessionStart(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gatewayrpc.McsrSnapshotReply{Error: "missing account or channel"}
	}
	user, err := p.fetchUser(ctx, account, req.IsPremium)
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gatewayrpc.McsrSnapshotReply{Error: msg}
		}
		p.log.Warn("mcsr snapshot fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.McsrSnapshotReply{Error: "stats lookup failed"}
	}
	if err := p.writeSnapshot(ctx, req.ChannelID, account, user); err != nil {
		p.log.Warn("mcsr snapshot write failed", zap.String("channel_id", req.ChannelID), zap.Error(err))
		return gatewayrpc.McsrSnapshotReply{Error: "snapshot store failed"}
	}
	return gatewayrpc.McsrSnapshotReply{Nickname: user.Nickname, Elo: user.Elo}
}

func (p *api) writeSnapshot(ctx context.Context, channelID, account string, user gatewayrpc.McsrUserReply) error {
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
// now".
func (p *api) session(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" || req.ChannelID == "" {
		return gatewayrpc.McsrSessionReply{Error: "missing account or channel"}
	}

	user, err := p.cachedUser(ctx, account, req.IsPremium)
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gatewayrpc.McsrSessionReply{Nickname: account, Error: msg}
		}
		p.log.Warn("mcsr session fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.McsrSessionReply{Nickname: account, Error: "stats lookup failed"}
	}

	var snap snapshot
	ok, err := p.cache.GetJSON(ctx, snapshotKey(req.ChannelID), &snap)
	if err != nil {
		p.log.Warn("mcsr snapshot read failed", zap.String("channel_id", req.ChannelID), zap.Error(err))
	}
	if !ok || snap.Account != strings.ToLower(account) {
		if werr := p.writeSnapshot(ctx, req.ChannelID, account, user); werr != nil {
			p.log.Warn("mcsr snapshot write failed", zap.String("channel_id", req.ChannelID), zap.Error(werr))
		}
		return gatewayrpc.McsrSessionReply{
			Nickname:    user.Nickname,
			Elo:         user.Elo,
			HasSnapshot: false,
			SinceUnix:   time.Now().Unix(),
		}
	}

	reply := gatewayrpc.McsrSessionReply{
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
