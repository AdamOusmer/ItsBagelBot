// Package urchin is the gateway provider for the urchin.gg Coral API: Hypixel
// Bed Wars session deltas (daily/weekly/monthly) and the Urchin cheater
// blacklist (sniper score, tags). Lifetime stats live in the hypixel provider:
// Coral's profile endpoint needs the Player Data permission our key does not
// carry, and the Hypixel API is a separate external system with its own budget.
//
// Every endpoint takes the player as Request.Account (a Minecraft username or
// UUID; Coral resolves usernames through Mojang) and answers a typed
// gatewayrpc reply. All endpoints are byte-flow: the reply — success or
// friendly failure (player not found) — is shaped and marshaled once on fetch,
// and a cache hit answers with the stored wire bytes untouched.
package urchin

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

	"ItsBagelBot/pkg/ratelimit"
)

// Cache TTLs. Session deltas move while the player is online, so they stay
// short; blacklist state can lag a little. The tags fetch (tagsTTL) doubles as
// the sniper endpoint's uuid source, so the two share one window.
const (
	sessionTTL  = 2 * time.Minute
	sniperTTL   = 10 * time.Minute
	tagsTTL     = 10 * time.Minute
	negativeTTL = 5 * time.Minute

	httpTimeout = 10 * time.Second
	// handlerTimeout leaves headroom over one upstream call plus the uuid
	// resolution hop the sniper endpoint needs.
	handlerTimeout = 15 * time.Second

	// rateWindowSeconds is the Coral key budget window (5 minutes).
	rateWindowSeconds = 300.0
)

// Config carries the provider's environment: the Coral base URL, the API key
// every request authenticates with, and the key's request budget per 5-minute
// window.
type Config struct {
	BaseURL   string
	APIKey    string
	RateLimit float64
}

// providerName is the subject token this provider answers under.
const providerName = "urchin"

// api holds the provider's runtime pieces; the declared endpoints capture it.
type api struct {
	http    *core.HTTPClient
	cache   *core.Cache
	key     string
	limiter *ratelimit.Limiter
	buckets core.Buckets
}

// New builds the urchin provider: the three session-delta endpoints plus the
// blacklist pair, all byte-flow. cfg.APIKey must be non-empty (providers.All
// skips the provider entirely when it is not configured).
func New(cfg Config, d provider.Deps) provider.Provider {
	p := newAPI(cfg, d)
	b := provider.NewProvider(providerName, d)
	for _, period := range []string{"daily", "weekly", "monthly"} {
		b.Endpoint(period).Timeout(handlerTimeout).
			Cached(sessionTTL, negativeTTL).
			Reply(sessionErrReply).
			Fallback("stats lookup failed").
			Fetch(p.sessionFetch(period))
	}
	b.Endpoint("sniper").Timeout(handlerTimeout).
		Cached(sniperTTL, negativeTTL).
		Reply(sniperErrReply).
		Fallback("score lookup failed").
		Fetch(p.sniperFetch)
	b.Endpoint("tags").Timeout(handlerTimeout).
		Cached(tagsTTL, negativeTTL).
		Reply(tagsErrReply).
		Fallback("tags lookup failed").
		Fetch(p.tagsFetch)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.urchin.gg"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 600
	}
	return &api{
		http:    core.NewHTTPClient(base, map[string]string{"X-API-Key": cfg.APIKey}, httpTimeout),
		cache:   d.Cache,
		key:     cfg.APIKey,
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gateway:urchin", cfg.RateLimit, rateWindowSeconds),
	}
}

func sessionErrReply(id, msg string) any {
	return gatewayrpc.UrchinSessionReply{Player: id, Error: msg}
}
func sniperErrReply(id, msg string) any { return gatewayrpc.UrchinSniperReply{Player: id, Error: msg} }
func tagsErrReply(id, msg string) any   { return gatewayrpc.UrchinTagsReply{Player: id, Error: msg} }

// account is a Minecraft player identifier (a username or UUID; Coral resolves
// usernames through Mojang) as supplied by the caller. It is a distinct type so
// the many handoffs below carry the player's meaning rather than a bare string.
type account string

func (a account) String() string { return string(a) }

// cacheKey normalizes the identifier for cache keys so "Player" and "player"
// share an entry.
func (a account) cacheKey() string { return strings.ToLower(strings.TrimSpace(string(a))) }

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

// sessionFetch spends one Coral token and pulls the period's session delta.
func (p *api) sessionFetch(period string) provider.FetchFunc {
	return func(ctx context.Context, req gatewayrpc.Request, id provider.ID) (any, error) {
		if err := p.buckets.Enforce(ctx, p.limiter, req.IsPremium); err != nil {
			return nil, err
		}
		return p.fetchSession(ctx, period, account(id.Display))
	}
}

func (p *api) fetchSession(ctx context.Context, period string, acct account) (gatewayrpc.UrchinSessionReply, error) {
	var resp sessionResponse
	q := url.Values{"player": {acct.String()}}
	if err := p.http.GetJSON(ctx, "/v3/player/sessions/"+period, q, &resp); err != nil {
		return gatewayrpc.UrchinSessionReply{}, err
	}

	reply := gatewayrpc.UrchinSessionReply{
		Player:    displayOr(resp.DisplayName, acct.String()),
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

func (p *api) fetchTags(ctx context.Context, acct account) (tagsResponse, error) {
	var resp tagsResponse
	return resp, p.http.GetJSON(ctx, "/v3/player/tags", url.Values{"player": {acct.String()}}, &resp)
}

// playerTags fetches the Coral tags response for acct behind a shared cache.
// Both the tags command and the sniper endpoint's uuid resolution read through
// it, so a player queried by either costs one /v3/player/tags call rather than
// two, and a missing player negative-caches once and satisfies both. It spends
// one rate-limit token only on a real upstream fetch (the enforce lives inside
// the cache's fill, so a hit costs nothing).
func (p *api) playerTags(ctx context.Context, acct account, isPremium bool) (tagsResponse, error) {
	key := core.Key(providerName, "playertags", acct.cacheKey())
	return core.Cached(ctx, p.cache, key, tagsTTL, negativeTTL, func(ctx context.Context) (tagsResponse, error) {
		if err := p.buckets.Enforce(ctx, p.limiter, isPremium); err != nil {
			return tagsResponse{}, err
		}
		return p.fetchTags(ctx, acct)
	})
}

// tagsFetch reads the shared tags cache and shapes the blacklist-tags reply.
func (p *api) tagsFetch(ctx context.Context, req gatewayrpc.Request, id provider.ID) (any, error) {
	acct := account(id.Display)
	resp, err := p.playerTags(ctx, acct, req.IsPremium)
	if err != nil {
		return nil, err
	}
	out := gatewayrpc.UrchinTagsReply{
		Player: displayOr(resp.DisplayName, acct.String()),
		Tags:   make([]gatewayrpc.UrchinTag, 0, len(resp.Tags)),
	}
	for _, t := range resp.Tags {
		out.Tags = append(out.Tags, gatewayrpc.UrchinTag{Type: t.TagType, Reason: t.Reason, AddedOn: t.AddedOn / 1000})
	}
	return out, nil
}

// cubelifyResponse is the Coral CubelifyResponse subset the gateway reads.
type cubelifyResponse struct {
	Score struct {
		Value float64 `json:"value"`
		Mode  string  `json:"mode"`
	} `json:"score"`
	Tags []json.RawMessage `json:"tags"`
}

// resolveUUID turns a username into the canonical uuid, reading through the
// shared tags cache (playerTags) so it reuses a lookup the tags command may
// already have made instead of dialing Coral again. An empty uuid on a 200 is
// shaped like a 404 so the downstream cubelify call is never made with a blank
// id.
func (p *api) resolveUUID(ctx context.Context, acct account, isPremium bool) (string, error) {
	resp, err := p.playerTags(ctx, acct, isPremium)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.UUID) == "" {
		return "", &core.UpstreamError{Status: 404, Message: "player not found"}
	}
	return resp.UUID, nil
}

// sniperFetch resolves the uuid through the shared tags cache, spends one
// Coral token, and pulls the cubelify sniper score.
func (p *api) sniperFetch(ctx context.Context, req gatewayrpc.Request, id provider.ID) (any, error) {
	acct := account(id.Display)
	uuid, err := p.resolveUUID(ctx, acct, req.IsPremium)
	if err != nil {
		return nil, err
	}
	if err := p.buckets.Enforce(ctx, p.limiter, req.IsPremium); err != nil {
		return nil, err
	}
	// The cubelify endpoint authenticates via the key query parameter (it is
	// built for the overlay); the client's X-API-Key header rides along too.
	var resp cubelifyResponse
	q := url.Values{"uuid": {uuid}, "key": {p.key}, "name": {acct.String()}}
	if err := p.http.GetJSON(ctx, "/v3/cubelify", q, &resp); err != nil {
		return nil, err
	}
	return gatewayrpc.UrchinSniperReply{
		Player:   acct.String(),
		Score:    resp.Score.Value,
		Mode:     resp.Score.Mode,
		TagCount: len(resp.Tags),
	}, nil
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
