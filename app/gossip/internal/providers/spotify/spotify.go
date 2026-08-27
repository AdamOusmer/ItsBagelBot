// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package spotify is the gossip provider for the Spotify Web API
// (api.spotify.com). It holds no Spotify credential of its own, not even an
// application. Every broadcaster registers their OWN Spotify app and connects
// their own account to it, so the client id, client secret and OAuth refresh
// token are all resolved just-in-time from the modules service
// (provider.Deps.SpotifyKeys) by the broadcaster id the caller passes as
// Request.ChannelID. That credential set is exchanged for a short-lived
// access token, cached per broadcaster until shortly before Spotify expires
// it; the plaintexts live only inside one token mint and are never cached or
// logged.
//
// Five endpoints: "search" resolves free text to tracks, "track" and "artist"
// look up bare catalog ids, "nowplaying" reads the broadcaster's
// currently-playing track, and "exchange" redeems the console's OAuth
// authorization code. That one lives here rather than in the console because
// the client secret it needs is imported by this service alone.
// Search and the lookups ride any valid user token; nowplaying additionally
// requires user-read-currently-playing (or user-read-playback-state) on the
// broadcaster's grant.
package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

const (
	// Catalog TTLs: track/artist metadata changes on album-drop timescales,
	// but nowplaying is asked about "what is playing right now", so it stays
	// short enough that a !song command never feels stale while still
	// collapsing a chatty audience onto one upstream call per window.
	searchTTL     = 10 * time.Minute
	trackTTL      = time.Hour
	nowplayingTTL = 15 * time.Second
	negativeTTL   = 15 * time.Second

	// httpTimeout bounds one Spotify call. Both hosts answer a healthy
	// request well inside a second; the budget matters most for the token
	// mint, which sits on the critical path of a cold call.
	httpTimeout = 6 * time.Second
	// lookupTimeout bounds one search/track/artist handler run (key resolve +
	// possible token mint + one data call). Kept under the dashboard's 9s RPC
	// budget, the same ceiling govee.devices answers within.
	lookupTimeout = 8 * time.Second
	// nowplayingTimeout bounds the nowplaying handler, which has the same
	// worst case as a lookup (cold token plus one data call).
	nowplayingTimeout = 8 * time.Second

	// Spotify publishes no fixed per-token request number: it throttles a
	// token that hammers the API with 429 + Retry-After. 30/min per
	// broadcaster is far above what chat-triggered lookups need and far below
	// where Spotify starts complaining; the bucket is per caller, so this is
	// each broadcaster's own ceiling, never a shared one.
	rateWindowSeconds = 60.0
	defaultRateLimit  = 30.0

	tokenPath      = "/api/token"
	searchPath     = "/v1/search"
	trackPath      = "/v1/tracks/"
	artistPath     = "/v1/artists/"
	albumPath      = "/v1/albums/"
	nowPlayingPath = "/v1/me/player/currently-playing"
	queuePath      = "/v1/me/player/queue"
	nextPath       = "/v1/me/player/next"

	bearerTokenType = "Bearer"

	defaultSearchLimit = 5
	maxSearchLimit     = 10

	// tokenExpirySkew is the clock-skew margin kept between a cached access
	// token's last certain-valid moment and Spotify's own expiry. See
	// tokenCacheTTL for why it is subtracted before halving rather than after.
	tokenExpirySkew = 60 * time.Second
	// minTokenTTL floors the cached-token window so a degenerate expires_in
	// cannot turn the cache into a churn loop (Spotify answers 3600).
	minTokenTTL = 30 * time.Second
)

// Config carries the provider's environment: the two Spotify hosts and the
// per-broadcaster request ceiling. There are no app credentials here: the
// fleet no longer owns a Spotify application. Every broadcaster registers
// their own and every credential, app included, is resolved per caller
// just-in-time.
type Config struct {
	BaseURL     string
	AccountsURL string
	RateLimit   float64
}

// providerName is the subject token this provider answers under.
const providerName = "spotify"

// api holds the provider's runtime pieces; the declared endpoints capture it.
// Every endpoint stays a bespoke handler: they must resolve the broadcaster's
// credential (and possibly mint a token) before any cache or upstream work,
// which the shared byte-flow skeleton deliberately does not model: a cached
// reply must never bypass the credential checks.
type api struct {
	// http dials api.spotify.com. No baked auth header: the access token is
	// per broadcaster (see bearerHeader).
	http *core.HTTPClient
	// auth dials accounts.spotify.com for the token mint; the app credentials
	// ride the form body, not a baked header.
	auth    *core.HTTPClient
	cache   *core.Cache
	keys    provider.SpotifyCredResolver
	log     *zap.Logger
	limiter *ratelimit.Limiter

	// buckets is the per-broadcaster budget template: the derived specs are
	// computed once here and re-keyed per caller (see rateAdmit).
	buckets core.Buckets
}

// New builds the spotify provider. d.SpotifyKeys must be present
// (providers.All skips the provider otherwise, since with no resolver it can
// authenticate nothing: the applications it exchanges against are the
// broadcasters' own, resolved through that same resolver).
func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	b.Endpoint("search").Timeout(lookupTimeout).Handle(p.search)
	b.Endpoint("track").Timeout(lookupTimeout).Handle(p.track)
	b.Endpoint("artist").Timeout(lookupTimeout).Handle(p.artist)
	b.Endpoint("nowplaying").Timeout(nowplayingTimeout).Handle(p.nowPlaying)
	b.Endpoint("exchange").Timeout(lookupTimeout).Handle(p.exchange)
	// Player writes: what makes !sr audible. Everything above only READS the
	// account; these two act on it, and they are what user-modify-playback-state
	// is for.
	b.Endpoint("queue").Timeout(nowplayingTimeout).Handle(p.queueTrack)
	b.Endpoint("next").Timeout(nowplayingTimeout).Handle(p.next)
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.spotify.com"
	}
	accounts := strings.TrimSuffix(cfg.AccountsURL, "/")
	if accounts == "" {
		accounts = "https://accounts.spotify.com"
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	return &api{
		http:    b.Client(base, nil, httpTimeout),
		auth:    b.Client(accounts, nil, httpTimeout),
		cache:   d.Cache,
		keys:    d.SpotifyKeys,
		log:     d.Logger(),
		limiter: d.Limiter,
		buckets: core.NewBuckets("ratelimit:gossip:spotify", cfg.RateLimit, rateWindowSeconds),
	}
}

// --- credentials -------------------------------------------------------------

// accessToken is a live Spotify bearer credential for ONE broadcaster,
// minted from their stored refresh token. It is its own type so the value
// riding between the mint, its cache and every data call cannot be
// silently confused with any other string on the way through.

// tokenResponse is the subset of POST /api/token we read.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	// Scope is the space-delimited set Spotify actually granted. It rides
	// both the code exchange and every refresh, and it is the only place the
	// truth lives: nothing else in the fleet can tell what an existing grant
	// covers.
	Scope string `json:"scope"`
	// RefreshToken appears only when Spotify ROTATES the refresh token; it
	// is written back to custody best-effort (see persistRotation).
	RefreshToken string `json:"refresh_token"`
}

// accessToken is a live Spotify bearer credential for ONE broadcaster,
// minted from their stored refresh token. It is its own type so the value
// riding between the mint, its cache and every data call cannot be silently
// confused with any other string on the way through.
type accessToken string

// accessToken returns a live access token for the broadcaster: minted from
// their refresh token, or served from the short-lived per-broadcaster cache.
func (p *api) accessToken(ctx context.Context, broadcaster string, creds core.SpotifyCredentials) (accessToken, error) {
	cacheKey := core.Key(providerName, "token", broadcaster)
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		tok, err := p.mintToken(ctx, broadcaster, creds)
		if err != nil {
			return nil, 0, err
		}
		return []byte(tok.AccessToken), tokenCacheTTL(tok.ExpiresIn), nil
	})
	if err != nil {
		return "", err
	}
	return accessToken(b), nil
}

// tokenCacheTTL shrinks Spotify's expires_in into a safe cache window. The
// byte-flow cache retains an entry for TWICE its fresh window (stale-while-
// revalidate), and serving an access token past Spotify's own expiry would
// 401 every call until the background refill lands, so the skew margin is
// subtracted BEFORE halving, which keeps fresh + retained at most at
// expiresIn - skew, strictly inside the token's real life.
func tokenCacheTTL(expiresIn int) time.Duration {
	ttl := (time.Duration(expiresIn)*time.Second - tokenExpirySkew) / 2
	if ttl < minTokenTTL {
		ttl = minTokenTTL
	}
	return ttl
}

// mintToken exchanges the broadcaster's refresh token for an access token
// using THEIR OWN app credentials (confidential-client form flow). The app is
// the one that issued the grant: Spotify rejects a refresh token presented by
// any other client id, which is why the credential set travels together.
//
// Spotify MAY rotate the refresh token on this exchange. Custody of the
// stored token belongs to the modules service, so the replacement is written
// back through its compare-and-swap rotate verb (see persistRotation),
// best-effort, never blocking the mint that already succeeded.
func (p *api) mintToken(ctx context.Context, broadcaster string, creds core.SpotifyCredentials) (tokenResponse, error) {
	tok, err := p.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
	})
	if err != nil {
		return tok, err
	}
	p.persistRotation(ctx, broadcaster, creds.RefreshToken, tok.RefreshToken)
	return tok, nil
}

// postToken runs one accounts.spotify.com token exchange, shared by the
// refresh-token mint and the console's authorization-code exchange: the two
// differ only in the form body.
func (p *api) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	var tok tokenResponse
	req := core.Request{
		Method: http.MethodPost,
		Path:   tokenPath,
		Headers: map[string]string{
			// The token endpoint takes a form body; core would otherwise set JSON.
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Body: []byte(form.Encode()),
	}
	if err := p.auth.Do(ctx, req, &tok); err != nil {
		return tok, err
	}
	if tok.AccessToken == "" {
		return tok, fmt.Errorf("spotify token mint: empty access_token")
	}
	return tok, nil
}

// accessTokenFor resolves the broadcaster's refresh token and returns a live
// access token, or ("", msg) with a reply-safe reason when it cannot.
func (p *api) accessTokenFor(ctx context.Context, broadcaster string) (tok accessToken, msg string) {
	creds, msg := p.credentials(ctx, broadcaster)
	if msg != "" {
		return "", msg
	}
	if creds.RefreshToken == "" {
		return "", "no Spotify connection on file"
	}
	tok, err := p.accessToken(ctx, broadcaster, creds)
	if err != nil {
		p.log.Warn("spotify token mint failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return "", friendlyAuthError(err)
	}
	return tok, ""
}

// credentials resolves the broadcaster's own application and grant, or
// ("", msg) with a reply-safe reason. A broadcaster who has not pasted their
// Spotify application yet is told so plainly: since the fleet retired its
// shared app, that is the first setup step and the most common miss.
func (p *api) credentials(ctx context.Context, broadcaster string) (core.SpotifyCredentials, string) {
	creds, err := p.keys.Credentials(ctx, broadcaster)
	if err != nil {
		p.log.Warn("spotify credential resolve failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return core.SpotifyCredentials{}, "could not read your Spotify connection"
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return core.SpotifyCredentials{}, "no Spotify app set up for this channel"
	}
	return creds, ""
}

// deadCredential reports the token-endpoint statuses that mean the stored
// grant itself is gone (revoked, expired, disconnected) rather than
// Spotify being unreachable. Recovery is a console reconnect, not a retry.
func deadCredential(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return true
	}
	return false
}

// friendlyAuthError maps a token-mint failure onto a chat-safe message. A
// dead credential says so plainly; anything else stays generic so an
// accounts.spotify.com outage never leaks detail.
func friendlyAuthError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && deadCredential(ue.Status) {
		return "your Spotify connection needs to be set up again"
	}
	return "could not reach Spotify"
}

// bearerHeader is the per-request authorization for data calls.
func bearerHeader(token accessToken) map[string]string {
	return map[string]string{"Authorization": bearerTokenType + " " + string(token)}
}

// rateAdmit spends one cache miss from the broadcaster's own budget. The
// bucket specs come pre-derived from the template; only the key is per caller.
func (p *api) rateAdmit(broadcaster string) func(context.Context) error {
	return func(ctx context.Context) error {
		return p.buckets.WithKey("ratelimit:gossip:spotify:"+broadcaster).Enforce(ctx, p.limiter, true)
	}
}

// fetchFailed maps a CachedBytes failure onto a typed error message. A
// budget denial or a friendly upstream failure has a message of its own
// (core.FriendlyUpstream); anything else is infrastructure and reports the
// endpoint's fallback after logging, mirroring what the FlowBuilder does for
// flow-declared endpoints.
func (p *api) fetchFailed(what, fallback string, err error) string {
	if msg, _ := core.FriendlyUpstream(err); msg != "" {
		return msg
	}
	p.log.Warn(what, zap.Error(err))
	return fallback
}

// --- shared upstream shapes --------------------------------------------------

// trackItem is Spotify's track object trimmed to what the module renders; it
// is the wire shape of both search results and the /tracks/{id} lookup, and
// nests as "item" in the currently-playing body.
type trackItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name   string `json:"name"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"album"`
	DurationMS   int64 `json:"duration_ms"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

// shapeTrack projects one upstream track onto the wire reply. Album art takes
// the FIRST image (Spotify orders widest first): consumers can downscale,
// never upscale.
func shapeTrack(it trackItem) *gossiprpc.SpotifyTrack {
	out := &gossiprpc.SpotifyTrack{
		ID:         it.ID,
		Name:       it.Name,
		Artists:    make([]string, 0, len(it.Artists)),
		Album:      it.Album.Name,
		DurationMS: it.DurationMS,
		URL:        it.ExternalURLs.Spotify,
	}
	for _, a := range it.Artists {
		out.Artists = append(out.Artists, a.Name)
	}
	if len(it.Album.Images) > 0 {
		out.ImageURL = it.Album.Images[0].URL
	}
	return out
}

// artistItem is Spotify's artist object trimmed the same way.
type artistItem struct {
	Name      string   `json:"name"`
	Genres    []string `json:"genres"`
	Followers struct {
		Total int64 `json:"total"`
	} `json:"followers"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

func shapeArtist(id string, it artistItem) *gossiprpc.SpotifyArtist {
	out := &gossiprpc.SpotifyArtist{
		ID:        id,
		Name:      it.Name,
		Genres:    it.Genres,
		Followers: it.Followers.Total,
		URL:       it.ExternalURLs.Spotify,
	}
	if len(it.Images) > 0 {
		out.ImageURL = it.Images[0].URL
	}
	return out
}

// validCatalogID reports whether id is a bare Spotify base62 catalog id. The
// id is concatenated into the request path, so anything outside [A-Za-z0-9]
// (full URIs, open.spotify.com links, traversal) is rejected rather than
// escaped: callers resolve URIs down to ids themselves.
func validCatalogID(id string) bool {
	if id == "" || len(id) > 22 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		default:
			return false
		}
	}
	return true
}

// --- search ------------------------------------------------------------------
//
// One endpoint answers every input shape a chat can produce: pasted Spotify
// links (page URLs, regional deep links, spotify:type:id URIs) resolve by
// catalog id in one direct immutable-object fetch, "song by artist" /
// "artist - song" text becomes a field-scoped search with a plain-text
// fallback, and anything else searches plainly. The route also picks the
// cache TTL: ids are forever, text answers drift with the catalog.

// searchResponse is the subset of GET /v1/search?type=track we read.
type searchResponse struct {
	Tracks struct {
		Items []trackItem `json:"items"`
	} `json:"tracks"`
}

// topTracksResponse is the subset of GET /v1/artists/{id}/top-tracks we read.
type topTracksResponse struct {
	Tracks []trackItem `json:"tracks"`
}

// albumResponse is the subset of GET /v1/albums/{id} we read. Its track items
// are the slim variant: names, artists, durations, links, but no album
// object and no artwork, so both are filled in from the album itself.
type albumResponse struct {
	Name   string `json:"name"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	Tracks struct {
		Items []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			DurationMS   int64 `json:"duration_ms"`
			ExternalURLs struct {
				Spotify string `json:"spotify"`
			} `json:"external_urls"`
		} `json:"items"`
	} `json:"tracks"`
}

func (p *api) search(ctx context.Context, req gossiprpc.Request) any {
	if msg := missingSearchInput(req); msg != "" {
		return gossiprpc.SpotifySearchReply{Error: msg}
	}
	broadcaster := strings.TrimSpace(req.ChannelID)

	target := classify(strings.TrimSpace(req.Query))

	// Said plainly BEFORE any credential work: an unsupported share should
	// not spend a token mint to end in results that merely echo its words.
	if target.kind == resolveUnsupportedLink {
		return gossiprpc.SpotifySearchReply{Error: "that Spotify link type isn't supported; share a track, artist or album"}
	}

	tok, msg := p.accessTokenFor(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.SpotifySearchReply{Error: msg}
	}

	scope := searchScope{
		broadcaster: broadcaster,
		limit:       clampSearchLimit(req.Limit),
		canonical:   target.cacheKey(),
		ttl:         searchTTL,
	}
	switch target.kind {
	case resolveTrackID:
		scope.ttl = trackTTL
		return p.searchCached(ctx, scope, p.trackByIDFetch(tok, target.id))
	case resolveArtistID:
		return p.searchCached(ctx, scope, p.artistTopFetch(tok, target.id, scope.limit))
	case resolveAlbumID:
		scope.ttl = trackTTL
		return p.searchCached(ctx, scope, p.albumTracksFetch(tok, target.id, scope.limit))
	default:
		return p.searchCached(ctx, scope, p.textFetch(tok, rawQuery(req), scope.limit))
	}
}

// missingSearchInput reports the reply error for a request missing either
// identifier every route needs, or "" when it is well-formed.
func missingSearchInput(req gossiprpc.Request) string {
	if strings.TrimSpace(req.Query) == "" {
		return "missing search query"
	}
	if strings.TrimSpace(req.ChannelID) == "" {
		return "missing channel"
	}
	return ""
}

// rawQuery re-reads the trimmed query for the text route; classify already
// normalized its own copy for cache-keying.
func rawQuery(req gossiprpc.Request) string {
	return strings.TrimSpace(req.Query)
}

func clampSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > maxSearchLimit {
		return maxSearchLimit
	}
	return limit
}

// searchScope bundles everything one classified branch needs to key and
// window its cache entry: broadcaster scoping, result cap, canonical input
// identity and freshness. The cache wrapper then takes two arguments instead
// of six loose primitives.
type searchScope struct {
	broadcaster string
	limit       int
	canonical   string
	ttl         time.Duration
}

// searchFetch produces one branch's typed reply on a cache miss. Each route
// is its own constructor capturing only what it needs (the access token, the
// catalog id), which keeps the dispatcher in search to if/switch statements
// alone.
type searchFetch func(context.Context) (gossiprpc.SpotifySearchReply, error)

// searchCached wraps one classified branch in the byte-flow cache. Results do
// not depend on who asked, but the quota spent does: keys stay
// broadcaster-scoped so one channel's cache entry can never be filled at
// another channel's expense (matching govee's rule), and admission stays nil
// because every branch spends the broadcaster's OWN token allowance against
// heavily cached reads.
func (p *api) searchCached(ctx context.Context, scope searchScope, fetch searchFetch) any {
	cacheKey := core.Key(providerName, "search",
		fmt.Sprintf("%s|%d|%s", scope.broadcaster, scope.limit, scope.canonical))
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReply(ctx, scope.ttl, negativeTTL,
			func(ctx context.Context) (any, error) { return fetch(ctx) },
			func(msg string) any { return gossiprpc.SpotifySearchReply{Error: msg} },
		)
		return b, ttl, err
	})
	if err != nil {
		return gossiprpc.SpotifySearchReply{Error: p.fetchFailed("spotify search fetch failed", "track search failed", err)}
	}
	return codec.RawMessage(b)
}

// trackByIDFetch resolves a pasted link straight to its immutable object.
func (p *api) trackByIDFetch(tok accessToken, id string) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		var it trackItem
		r := core.Request{Method: http.MethodGet, Path: trackPath + id, Headers: bearerHeader(tok)}
		if err := p.http.Do(ctx, r, &it); err != nil {
			return gossiprpc.SpotifySearchReply{}, err
		}
		return gossiprpc.SpotifySearchReply{
			ResolvedAs: viaTrackLink,
			Tracks:     []gossiprpc.SpotifyTrack{*shapeTrack(it)},
		}, nil
	}
}

// artistTopFetch serves an artist link as their current top tracks, capped at
// the caller's limit (the upstream window is fixed at ten).
func (p *api) artistTopFetch(tok accessToken, id string, limit int) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		var resp topTracksResponse
		r := core.Request{Method: http.MethodGet, Path: artistPath + id + "/top-tracks", Headers: bearerHeader(tok)}
		if err := p.http.Do(ctx, r, &resp); err != nil {
			return gossiprpc.SpotifySearchReply{}, err
		}
		reply := gossiprpc.SpotifySearchReply{
			ResolvedAs: viaArtistTop,
			Tracks:     make([]gossiprpc.SpotifyTrack, 0, len(resp.Tracks)),
		}
		for _, it := range resp.Tracks {
			reply.Tracks = append(reply.Tracks, *shapeTrack(it))
		}
		return truncateTracks(reply, limit), nil
	}
}

// albumTracksFetch lists an album capped at the caller's limit.
func (p *api) albumTracksFetch(tok accessToken, id string, limit int) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		return p.albumTracks(ctx, tok, id, limit)
	}
}

// textFetch runs the ordered candidate plan for free-text inputs.
func (p *api) textFetch(tok accessToken, raw string, limit int) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		return p.searchText(ctx, tok, planTextSearch(raw), limit)
	}
}

// searchText walks the plan in order: the first candidate returning tracks
// wins, an upstream failure aborts outright (infrastructure is not answered
// by quietly degrading further down the plan), and a plan exhausted without a
// hit returns the final empty reply: no result is an answer, not an error.
func (p *api) searchText(ctx context.Context, tok accessToken, plan []searchCandidate, limit int) (gossiprpc.SpotifySearchReply, error) {
	var last gossiprpc.SpotifySearchReply
	for _, c := range plan {
		reply, err := p.runSearch(ctx, tok, c, limit)
		if err != nil {
			return gossiprpc.SpotifySearchReply{}, err
		}
		last = reply
		if len(reply.Tracks) > 0 {
			return reply, nil
		}
	}
	return last, nil
}

func (p *api) runSearch(ctx context.Context, tok accessToken, c searchCandidate, limit int) (gossiprpc.SpotifySearchReply, error) {
	q := url.Values{"q": {c.q}, "type": {"track"}, "limit": {strconv.Itoa(limit)}}
	var resp searchResponse
	r := core.Request{Method: http.MethodGet, Path: searchPath, Query: q, Headers: bearerHeader(tok)}
	if err := p.http.Do(ctx, r, &resp); err != nil {
		return gossiprpc.SpotifySearchReply{}, err
	}
	reply := gossiprpc.SpotifySearchReply{
		ResolvedAs: c.name,
		Tracks:     make([]gossiprpc.SpotifyTrack, 0, len(resp.Tracks.Items)),
	}
	for _, it := range resp.Tracks.Items {
		reply.Tracks = append(reply.Tracks, *shapeTrack(it))
	}
	return reply, nil
}

// albumTracks lists an album's tracks capped at limit, stamping the album
// name and artwork onto every entry since the slim track objects lack them.
func (p *api) albumTracks(ctx context.Context, tok accessToken, id string, limit int) (gossiprpc.SpotifySearchReply, error) {
	var resp albumResponse
	r := core.Request{Method: http.MethodGet, Path: albumPath + id, Headers: bearerHeader(tok)}
	if err := p.http.Do(ctx, r, &resp); err != nil {
		return gossiprpc.SpotifySearchReply{}, err
	}
	reply := gossiprpc.SpotifySearchReply{
		ResolvedAs: viaAlbum,
		Tracks:     make([]gossiprpc.SpotifyTrack, 0, len(resp.Tracks.Items)),
	}
	image := ""
	if len(resp.Images) > 0 {
		image = resp.Images[0].URL
	}
	for _, it := range resp.Tracks.Items {
		track := gossiprpc.SpotifyTrack{
			ID:         it.ID,
			Name:       it.Name,
			Artists:    make([]string, 0, len(it.Artists)),
			Album:      resp.Name,
			DurationMS: it.DurationMS,
			URL:        it.ExternalURLs.Spotify,
			ImageURL:   image,
		}
		for _, a := range it.Artists {
			track.Artists = append(track.Artists, a.Name)
		}
		reply.Tracks = append(reply.Tracks, track)
	}
	return truncateTracks(reply, limit), nil
}

// truncateTracks caps a reply at limit: top-tracks and album listings come
// back in fixed-size upstream windows regardless of the requested count.
func truncateTracks(reply gossiprpc.SpotifySearchReply, limit int) gossiprpc.SpotifySearchReply {
	if len(reply.Tracks) > limit {
		reply.Tracks = reply.Tracks[:limit]
	}
	return reply
}

// --- track / artist ----------------------------------------------------------

func (p *api) track(ctx context.Context, req gossiprpc.Request) any {
	id := strings.TrimSpace(req.TrackID)
	if !validCatalogID(id) {
		return gossiprpc.SpotifyTrackReply{Error: "invalid track id"}
	}
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.SpotifyTrackReply{Error: "missing channel"}
	}

	tok, msg := p.accessTokenFor(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.SpotifyTrackReply{Error: msg}
	}

	cacheKey := core.Key(providerName, "track", broadcaster+"|"+id)
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReply(ctx, trackTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				var it trackItem
				req := core.Request{Method: http.MethodGet, Path: trackPath + id, Headers: bearerHeader(tok)}
				if err := p.http.Do(ctx, req, &it); err != nil {
					return nil, err
				}
				return gossiprpc.SpotifyTrackReply{Track: shapeTrack(it)}, nil
			},
			func(msg string) any { return gossiprpc.SpotifyTrackReply{Error: msg} },
		)
		return b, ttl, err
	})
	if err != nil {
		return gossiprpc.SpotifyTrackReply{Error: p.fetchFailed("spotify track fetch failed", "track lookup failed", err)}
	}
	return codec.RawMessage(b)
}

// artistResponse wraps the bare artist object GET /v1/artists/{id} answers.
type artistResponse struct {
	artistItem
	ID string `json:"id"`
}

func (p *api) artist(ctx context.Context, req gossiprpc.Request) any {
	id := strings.TrimSpace(req.ArtistID)
	if !validCatalogID(id) {
		return gossiprpc.SpotifyArtistReply{Error: "invalid artist id"}
	}
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.SpotifyArtistReply{Error: "missing channel"}
	}

	tok, msg := p.accessTokenFor(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.SpotifyArtistReply{Error: msg}
	}

	cacheKey := core.Key(providerName, "artist", broadcaster+"|"+id)
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReply(ctx, trackTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				var resp artistResponse
				req := core.Request{Method: http.MethodGet, Path: artistPath + id, Headers: bearerHeader(tok)}
				if err := p.http.Do(ctx, req, &resp); err != nil {
					return nil, err
				}
				return gossiprpc.SpotifyArtistReply{Artist: shapeArtist(resp.ID, resp.artistItem)}, nil
			},
			func(msg string) any { return gossiprpc.SpotifyArtistReply{Error: msg} },
		)
		return b, ttl, err
	})
	if err != nil {
		return gossiprpc.SpotifyArtistReply{Error: p.fetchFailed("spotify artist fetch failed", "artist lookup failed", err)}
	}
	return codec.RawMessage(b)
}

// --- nowplaying --------------------------------------------------------------

// nowPlayingResponse is the subset of GET /v1/me/player/currently-playing we
// read. Spotify answers 204 with no body when playback is idle or private;
// core decodes that as a zero value, which reads back here as IsPlaying false
// with no item, exactly the right shape for "nothing playing".
type nowPlayingResponse struct {
	IsPlaying  bool      `json:"is_playing"`
	ProgressMS int64     `json:"progress_ms"`
	Item       trackItem `json:"item"`
}

func (p *api) nowPlaying(ctx context.Context, req gossiprpc.Request) any {
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.SpotifyNowPlayingReply{Error: "missing channel"}
	}

	tok, msg := p.accessTokenFor(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.SpotifyNowPlayingReply{Error: msg}
	}

	// Unlike the catalog reads this endpoint is polled: chat asks many times
	// a minute against ONE broadcaster's token allowance, so its miss path
	// admits through their bucket: a flood degrades to the friendly busy
	// message instead of burning the token Spotify throttles.
	b, err := core.CachedBytes(ctx, p.cache, core.Key(providerName, "nowplaying", broadcaster), p.rateAdmit(broadcaster),
		func(ctx context.Context) ([]byte, time.Duration, error) {
			b, ttl, _, err := core.BuildReply(ctx, nowplayingTTL, negativeTTL,
				func(ctx context.Context) (any, error) {
					var resp nowPlayingResponse
					req := core.Request{Method: http.MethodGet, Path: nowPlayingPath, Headers: bearerHeader(tok)}
					if err := p.http.Do(ctx, req, &resp); err != nil {
						return nil, err
					}
					reply := gossiprpc.SpotifyNowPlayingReply{IsPlaying: resp.IsPlaying, ProgressMS: resp.ProgressMS}
					if resp.IsPlaying && resp.Item.ID != "" {
						reply.Track = shapeTrack(resp.Item)
					}
					return reply, nil
				},
				func(msg string) any { return gossiprpc.SpotifyNowPlayingReply{Error: msg} },
			)
			return b, ttl, err
		})
	if err != nil {
		return gossiprpc.SpotifyNowPlayingReply{Error: p.fetchFailed("spotify now-playing fetch failed", "could not reach Spotify", err)}
	}
	return codec.RawMessage(b)
}

// --- authorization-code exchange ---------------------------------------------

// exchange completes the console's Spotify connect flow: the browser hands the
// console an authorization code, and the console forwards it here rather than
// redeeming it itself. Gossip is the only service holding a broadcaster's
// client secret in plaintext (it already needs it to refresh their token), so
// routing the redemption through this endpoint keeps the secret out of the
// browser-facing app entirely: the console only ever sees the refresh token,
// which it already stores through the modules custody RPC.
//
// The redirect_uri travels from the caller because Spotify validates it
// against the exact value used in the authorize step; the console owns that
// URL, and a mismatched one fails at Spotify rather than here.
func (p *api) exchange(ctx context.Context, req gossiprpc.Request) any {
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.SpotifyExchangeReply{Error: "missing channel"}
	}
	code := strings.TrimSpace(req.Code)
	redirect := strings.TrimSpace(req.RedirectURI)
	if code == "" || redirect == "" {
		return gossiprpc.SpotifyExchangeReply{Error: "missing authorization code"}
	}

	creds, msg := p.credentials(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.SpotifyExchangeReply{Error: msg}
	}

	tok, err := p.postToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
	})
	if err != nil {
		p.log.Warn("spotify code exchange failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return gossiprpc.SpotifyExchangeReply{Error: friendlyAuthError(err)}
	}

	// Spotify reuses consent: a re-connect with unchanged scopes comes back
	// with no refresh token. That is not an error: the caller keeps whatever
	// is already on file, so it is reported as an empty token, and the
	// console decides whether a stored one makes it a success.
	return gossiprpc.SpotifyExchangeReply{RefreshToken: tok.RefreshToken, Scopes: splitScopes(tok.Scope)}
}

// splitScopes turns Spotify's space-delimited scope string into the list the
// console stores. Spotify has also been seen answering with a comma in the
// middle of an otherwise space-delimited string, so both separate.
func splitScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' })
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// --- player control ----------------------------------------------------------

// playerWrite runs one player mutation for a broadcaster: resolve their token,
// admit through their rate bucket, POST, and map the failure onto a chat-safe
// reason. No cache on purpose: a write that "hits" is a write that did not
// happen.
func (p *api) playerWrite(ctx context.Context, req gossiprpc.Request, do func(context.Context, accessToken) error) any {
	broadcaster := strings.TrimSpace(req.ChannelID)
	if broadcaster == "" {
		return gossiprpc.SpotifyPlayerReply{Error: "missing channel"}
	}
	tok, msg := p.accessTokenFor(ctx, broadcaster)
	if msg != "" {
		return gossiprpc.SpotifyPlayerReply{Error: msg}
	}
	if err := p.rateAdmit(broadcaster)(ctx); err != nil {
		return gossiprpc.SpotifyPlayerReply{Error: p.fetchFailed("spotify player write denied", "Spotify is busy right now, try again in a moment", err)}
	}
	if err := do(ctx, tok); err != nil {
		return gossiprpc.SpotifyPlayerReply{Error: p.playerFailed(err)}
	}
	return gossiprpc.SpotifyPlayerReply{}
}

// playerFailed maps a player-write failure onto the reason chat can act on.
// The three statuses below are the three real-world states a broadcaster can
// fix themselves; anything else stays generic so an outage leaks no detail.
func (p *api) playerFailed(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		switch ue.Status {
		case http.StatusNotFound:
			// Spotify's NO_ACTIVE_DEVICE: the account is fine, nothing is
			// listening. Playing anything on any device fixes it.
			return "no active Spotify device, start playing something first"
		case http.StatusForbidden:
			// Spotify answers 403 for more than one reason: a free account
			// (player control is Premium-only upstream), a grant minted
			// before playback control was requested (Spotify's own
			// "Insufficient client scope" text), and, distinctly, a
			// development-mode app whose caller is not on its user
			// allowlist. Only the first two are things reconnecting on the
			// dashboard fixes; mapping every 403 to that message would tell
			// an allowlist-blocked broadcaster to redo a step that was never
			// broken.
			msg := strings.ToUpper(ue.Message)
			switch {
			case strings.Contains(msg, "PREMIUM"):
				return "Spotify Premium is required for queue control"
			case strings.Contains(msg, "SCOPE"):
				return "the Spotify connection is missing playback control, reconnect it on the dashboard"
			}
		case http.StatusUnauthorized:
			return "your Spotify connection needs to be set up again"
		}
	}
	return p.fetchFailed("spotify player write failed", "could not reach Spotify", err)
}

// queueTrack backs spotify.queue: append one track to whatever device the
// broadcaster is playing on. Spotify addresses the track by full URI.
func (p *api) queueTrack(ctx context.Context, req gossiprpc.Request) any {
	id := strings.TrimSpace(req.TrackID)
	if id == "" {
		return gossiprpc.SpotifyPlayerReply{Error: "missing track"}
	}
	return p.playerWrite(ctx, req, func(ctx context.Context, tok accessToken) error {
		q := url.Values{"uri": {"spotify:track:" + id}}
		return p.http.Do(ctx, core.Request{Method: http.MethodPost, Path: queuePath, Query: q, Headers: bearerHeader(tok)}, nil)
	})
}

// next backs spotify.next: skip the playing track. The queue head Spotify
// promotes is its own (which includes everything queueTrack pushed), so the
// caller advancing its request list in step stays truthful.
func (p *api) next(ctx context.Context, req gossiprpc.Request) any {
	return p.playerWrite(ctx, req, func(ctx context.Context, tok accessToken) error {
		return p.http.Do(ctx, core.Request{Method: http.MethodPost, Path: nextPath, Headers: bearerHeader(tok)}, nil)
	})
}
