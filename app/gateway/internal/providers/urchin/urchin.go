// Package urchin is the gateway provider for the urchin.gg Coral API: Hypixel
// Bed Wars stats (daily/weekly/monthly session deltas, lifetime stats) and the
// Urchin cheater blacklist (sniper score, tags).
//
// Every endpoint takes the player as Request.Account (a Minecraft username or
// UUID; Coral resolves usernames through Mojang) and answers a typed
// gatewayrpc reply. A well-known upstream failure (404/400 player not found)
// becomes a reply-level Error, not an RPC failure, so sesame can chat it back.
package urchin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// Cache TTLs. Session deltas move while the player is online, so they stay
// short; lifetime stats and blacklist state can lag a little. The uuid
// resolution never changes for a username short of a Mojang rename, so it is
// cached long.
const (
	sessionTTL = 2 * time.Minute
	statsTTL   = 10 * time.Minute
	sniperTTL  = 10 * time.Minute
	tagsTTL    = 10 * time.Minute
	uuidTTL    = 24 * time.Hour

	// profileCacheAge is passed as Coral's max_cache_age so it serves its own
	// stored Hypixel snapshot instead of hitting Hypixel per lookup.
	profileCacheAge = "10m"

	httpTimeout = 10 * time.Second
	// handlerTimeout leaves headroom over one upstream call plus the uuid
	// resolution hop the sniper endpoint needs.
	handlerTimeout = 15 * time.Second
)

// Config carries the provider's environment: the Coral base URL and the API
// key every request authenticates with.
type Config struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
}

// Provider implements provider.Provider for the Coral API.
type Provider struct {
	http  *core.HTTPClient
	cache *core.Cache
	key   string
	log   *zap.Logger

	limiter      *ratelimit.Limiter
	generalSpec  ratelimit.Spec
	standardSpec ratelimit.Spec
}

// New builds the urchin provider. cfg.APIKey must be non-empty (main skips the
// provider entirely when it is not configured).
func New(cfg Config, cache *core.Cache, limiter *ratelimit.Limiter, log *zap.Logger) *Provider {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.urchin.gg"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 500
	}
	generalCapacity := cfg.RateLimit
	standardCapacity := cfg.RateLimit * 0.75
	return &Provider{
		http:  core.NewHTTPClient(base, map[string]string{"X-API-Key": cfg.APIKey}, httpTimeout),
		cache: cache,
		key:   cfg.APIKey,
		log:   log,

		limiter:      limiter,
		generalSpec:  ratelimit.NewSpec(generalCapacity, generalCapacity/600.0),
		standardSpec: ratelimit.NewSpec(standardCapacity, standardCapacity/600.0),
	}
}

func (p *Provider) Name() string { return "urchin" }

func (p *Provider) Endpoints() []provider.Endpoint {
	return []provider.Endpoint{
		{Name: "daily", Timeout: handlerTimeout, Handle: p.session("daily")},
		{Name: "weekly", Timeout: handlerTimeout, Handle: p.session("weekly")},
		{Name: "monthly", Timeout: handlerTimeout, Handle: p.session("monthly")},
		{Name: "stats", Timeout: handlerTimeout, Handle: p.stats},
		{Name: "sniper", Timeout: handlerTimeout, Handle: p.sniper},
		{Name: "tags", Timeout: handlerTimeout, Handle: p.tags},
	}
}

// accountKey normalizes the player identifier for cache keys so "Player" and
// "player" share an entry.
func accountKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

// enforceRateLimit consumes from the provider's token buckets. Standard requests
// must pass both their restricted bucket and the general bucket. Premium requests
// only consume from the general bucket, enjoying the 25% reserve.
func (p *Provider) enforceRateLimit(ctx context.Context, isPremium bool) error {
	if p.limiter == nil {
		return nil
	}
	generalReq := ratelimit.Request{Key: "ratelimit:gateway:urchin", Spec: p.generalSpec}
	
	if isPremium {
		ok, err := p.limiter.Allow(ctx, generalReq)
		if err != nil {
			return err
		}
		if !ok {
			return &core.UpstreamError{Status: 429, Message: "premium rate limit exceeded"}
		}
		return nil
	}

	standardReq := ratelimit.Request{Key: "ratelimit:gateway:urchin:standard", Spec: p.standardSpec}
	deniedIdx, err := p.limiter.AllowOrdered(ctx, standardReq, generalReq)
	if err != nil {
		return err
	}
	if deniedIdx != 0 {
		return &core.UpstreamError{Status: 429, Message: "standard rate limit exceeded"}
	}
	return nil
}

// friendlyError maps an upstream failure onto a user-facing reply error, or
// returns "" for an infrastructure failure the caller should propagate.
func friendlyError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		switch ue.Status {
		case 400, 404:
			if ue.Message != "" {
				return ue.Message
			}
			return "player not found"
		case 429:
			return "stats service is busy, try again in a minute"
		}
	}
	return ""
}

// --- session deltas (daily / weekly / monthly) -------------------------------

// sessionResponse is the Coral SessionDeltaResponse subset the gateway reads.
// Delta is the recursive diff of the Hypixel player object: unchanged fields
// omitted, changed numeric stats as bare numbers, non-numeric changes as
// {old,new} objects.
type sessionResponse struct {
	UUID        string          `json:"uuid"`
	DisplayName *string         `json:"displayname"`
	From        int64           `json:"from"`
	Delta       json.RawMessage `json:"delta"`
}

// sessionDelta is the Bed Wars slice of the diff. Fields use json.RawMessage +
// numDelta because a stat can surface as a bare number or, for a returning
// player diffed against a partial snapshot, as an {old,new} object we skip.
type sessionDelta struct {
	Stats struct {
		Bedwars map[string]json.RawMessage `json:"Bedwars"`
	} `json:"stats"`
	Achievements map[string]json.RawMessage `json:"achievements"`
}

// numDelta reads a numeric session diff. Bare numbers are true period deltas;
// the {old,new} object form means the baseline was missing (a lifetime total
// would masquerade as a period gain), so it reads as 0.
func numDelta(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	f, err := n.Float64()
	if err != nil {
		return 0
	}
	return int64(f)
}

func (p *Provider) session(period string) func(context.Context, gatewayrpc.Request) any {
	return func(ctx context.Context, req gatewayrpc.Request) any {
		account := strings.TrimSpace(req.Account)
		if account == "" {
			return gatewayrpc.UrchinSessionReply{Error: "missing account"}
		}
		key := core.Key(p.Name(), period, accountKey(account))
		reply, err := core.Cached(ctx, p.cache, key, sessionTTL, 5*time.Minute, func(ctx context.Context) (gatewayrpc.UrchinSessionReply, error) {
			if err := p.enforceRateLimit(ctx, req.IsPremium); err != nil {
				return gatewayrpc.UrchinSessionReply{}, err
			}
			return p.fetchSession(ctx, period, account)
		})
		if err != nil {
			if msg := friendlyError(err); msg != "" {
				return gatewayrpc.UrchinSessionReply{Player: account, Error: msg}
			}
			p.log.Warn("urchin session fetch failed", zap.String("period", period), zap.String("account", account), zap.Error(err))
			return gatewayrpc.UrchinSessionReply{Player: account, Error: "stats lookup failed"}
		}
		return reply
	}
}

func (p *Provider) fetchSession(ctx context.Context, period, account string) (gatewayrpc.UrchinSessionReply, error) {
	var resp sessionResponse
	q := url.Values{"player": {account}}
	if err := p.http.GetJSON(ctx, "/v3/player/sessions/"+period, q, &resp); err != nil {
		return gatewayrpc.UrchinSessionReply{}, err
	}

	reply := gatewayrpc.UrchinSessionReply{
		Player:    displayOr(resp.DisplayName, account),
		SinceUnix: resp.From / 1000, // Coral timestamps are Unix milliseconds
	}
	if len(resp.Delta) > 0 {
		var d sessionDelta
		if err := json.Unmarshal(resp.Delta, &d); err == nil {
			bw := d.Stats.Bedwars
			reply.Wins = numDelta(bw["wins_bedwars"])
			reply.Losses = numDelta(bw["losses_bedwars"])
			reply.FinalKills = numDelta(bw["final_kills_bedwars"])
			reply.FinalDeaths = numDelta(bw["final_deaths_bedwars"])
			reply.BedsBroken = numDelta(bw["beds_broken_bedwars"])
			reply.GamesPlayed = numDelta(bw["games_played_bedwars"])
			reply.Levels = numDelta(d.Achievements["bedwars_level"])
		}
	}
	return reply, nil
}

// --- lifetime stats -----------------------------------------------------------

// profileResponse is the Coral PlayerStatsResponse subset the gateway reads:
// the resolved name plus the raw Hypixel player object.
type profileResponse struct {
	Username    string  `json:"username"`
	DisplayName *string `json:"displayname"`
	Hypixel     struct {
		Achievements struct {
			BedwarsLevel int64 `json:"bedwars_level"`
		} `json:"achievements"`
		Stats struct {
			Bedwars struct {
				Wins        int64 `json:"wins_bedwars"`
				Losses      int64 `json:"losses_bedwars"`
				FinalKills  int64 `json:"final_kills_bedwars"`
				FinalDeaths int64 `json:"final_deaths_bedwars"`
				BedsBroken  int64 `json:"beds_broken_bedwars"`
			} `json:"Bedwars"`
		} `json:"stats"`
	} `json:"hypixel"`
}

func (p *Provider) stats(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.UrchinStatsReply{Error: "missing account"}
	}
	key := core.Key(p.Name(), "stats", accountKey(account))
	reply, err := core.Cached(ctx, p.cache, key, statsTTL, 5*time.Minute, func(ctx context.Context) (gatewayrpc.UrchinStatsReply, error) {
		if err := p.enforceRateLimit(ctx, req.IsPremium); err != nil {
			return gatewayrpc.UrchinStatsReply{}, err
		}
		var resp profileResponse
		q := url.Values{"player": {account}, "max_cache_age": {profileCacheAge}}
		if err := p.http.GetJSON(ctx, "/v3/player/profile", q, &resp); err != nil {
			return gatewayrpc.UrchinStatsReply{}, err
		}
		name := displayOr(resp.DisplayName, resp.Username)
		if name == "" {
			name = account
		}
		bw := resp.Hypixel.Stats.Bedwars
		return gatewayrpc.UrchinStatsReply{
			Player:      name,
			Stars:       resp.Hypixel.Achievements.BedwarsLevel,
			Wins:        bw.Wins,
			Losses:      bw.Losses,
			FinalKills:  bw.FinalKills,
			FinalDeaths: bw.FinalDeaths,
			BedsBroken:  bw.BedsBroken,
		}, nil
	})
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gatewayrpc.UrchinStatsReply{Player: account, Error: msg}
		}
		p.log.Warn("urchin stats fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.UrchinStatsReply{Player: account, Error: "stats lookup failed"}
	}
	return reply
}

// --- blacklist: tags + sniper score -------------------------------------------

// tagsResponse is the Coral PlayerTagsResponse subset the gateway reads. It is
// also the uuid resolver for the sniper endpoint (it accepts a username and
// echoes the canonical uuid, without needing the Player Data permission the
// dedicated /v3/resolve endpoint requires).
type tagsResponse struct {
	UUID        string  `json:"uuid"`
	DisplayName *string `json:"displayname"`
	Tags        []struct {
		TagType string `json:"tag_type"`
		Reason  string `json:"reason"`
		AddedOn int64  `json:"added_on"`
	} `json:"tags"`
}

func (p *Provider) fetchTags(ctx context.Context, account string) (tagsResponse, error) {
	var resp tagsResponse
	return resp, p.http.GetJSON(ctx, "/v3/player/tags", url.Values{"player": {account}}, &resp)
}

func (p *Provider) tags(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.UrchinTagsReply{Error: "missing account"}
	}
	key := core.Key(p.Name(), "tags", accountKey(account))
	reply, err := core.Cached(ctx, p.cache, key, tagsTTL, 5*time.Minute, func(ctx context.Context) (gatewayrpc.UrchinTagsReply, error) {
		if err := p.enforceRateLimit(ctx, req.IsPremium); err != nil {
			return gatewayrpc.UrchinTagsReply{}, err
		}
		resp, err := p.fetchTags(ctx, account)
		if err != nil {
			return gatewayrpc.UrchinTagsReply{}, err
		}
		out := gatewayrpc.UrchinTagsReply{
			Player: displayOr(resp.DisplayName, account),
			Tags:   make([]gatewayrpc.UrchinTag, 0, len(resp.Tags)),
		}
		for _, t := range resp.Tags {
			out.Tags = append(out.Tags, gatewayrpc.UrchinTag{Type: t.TagType, Reason: t.Reason, AddedOn: t.AddedOn / 1000})
		}
		return out, nil
	})
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gatewayrpc.UrchinTagsReply{Player: account, Error: msg}
		}
		p.log.Warn("urchin tags fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.UrchinTagsReply{Player: account, Error: "tags lookup failed"}
	}
	return reply
}

// cubelifyResponse is the Coral CubelifyResponse subset the gateway reads.
type cubelifyResponse struct {
	Score struct {
		Value float64 `json:"value"`
		Mode  string  `json:"mode"`
	} `json:"score"`
	Tags []json.RawMessage `json:"tags"`
}

// resolveUUID turns a username into the canonical uuid via the tags endpoint,
// cached for a day.
func (p *Provider) resolveUUID(ctx context.Context, account string, isPremium bool) (string, error) {
	key := core.Key(p.Name(), "uuid", accountKey(account))
	return core.Cached(ctx, p.cache, key, uuidTTL, 5*time.Minute, func(ctx context.Context) (string, error) {
		if err := p.enforceRateLimit(ctx, isPremium); err != nil {
			return "", err
		}
		resp, err := p.fetchTags(ctx, account)
		if err != nil {
			return "", err
		}
		return resp.UUID, nil
	})
}

func (p *Provider) sniper(ctx context.Context, req gatewayrpc.Request) any {
	account := strings.TrimSpace(req.Account)
	if account == "" {
		return gatewayrpc.UrchinSniperReply{Error: "missing account"}
	}
	key := core.Key(p.Name(), "sniper", accountKey(account))
	reply, err := core.Cached(ctx, p.cache, key, sniperTTL, 5*time.Minute, func(ctx context.Context) (gatewayrpc.UrchinSniperReply, error) {
		uuid, err := p.resolveUUID(ctx, account, req.IsPremium)
		if err != nil {
			return gatewayrpc.UrchinSniperReply{}, err
		}
		if err := p.enforceRateLimit(ctx, req.IsPremium); err != nil {
			return gatewayrpc.UrchinSniperReply{}, err
		}
		// The cubelify endpoint authenticates via the key query parameter (it is
		// built for the overlay); the client's X-API-Key header rides along too.
		var resp cubelifyResponse
		q := url.Values{"uuid": {uuid}, "key": {p.key}, "name": {account}}
		if err := p.http.GetJSON(ctx, "/v3/cubelify", q, &resp); err != nil {
			return gatewayrpc.UrchinSniperReply{}, err
		}
		return gatewayrpc.UrchinSniperReply{
			Player:   account,
			Score:    resp.Score.Value,
			Mode:     resp.Score.Mode,
			TagCount: len(resp.Tags),
		}, nil
	})
	if err != nil {
		if msg := friendlyError(err); msg != "" {
			return gatewayrpc.UrchinSniperReply{Player: account, Error: msg}
		}
		p.log.Warn("urchin sniper fetch failed", zap.String("account", account), zap.Error(err))
		return gatewayrpc.UrchinSniperReply{Player: account, Error: "score lookup failed"}
	}
	return reply
}

// displayOr prefers the API's display name when present and non-empty.
// Minecraft color codes (§X) are stripped so Twitch chat gets a clean name.
func displayOr(display *string, fallback string) string {
	if display != nil && *display != "" {
		return stripMinecraftCodes(*display)
	}
	return fallback
}

// stripMinecraftCodes removes Minecraft §X formatting sequences (section sign
// followed by one character) from s. Returns s unchanged when no codes are
// present.
func stripMinecraftCodes(s string) string {
	if !strings.Contains(s, "§") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	skip := false
	for _, r := range s {
		if skip {
			skip = false
			continue
		}
		if r == '§' {
			skip = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
