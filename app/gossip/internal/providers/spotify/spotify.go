// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package spotify is the gossip provider for the Spotify Web API
// (api.spotify.com). Like govee it holds no per-broadcaster secret of its
// own: every call authenticates as a broadcaster, whose OAuth refresh token
// is resolved just-in-time from the modules service
// (provider.Deps.SpotifyKeys) by the broadcaster id the caller passes as
// Request.ChannelID. Gossip holds only the fleet's own Spotify app
// credentials (Config.ClientID/ClientSecret) and exchanges each
// broadcaster's refresh token for a short-lived access token, cached per
// broadcaster until shortly before Spotify expires it. The plaintext refresh
// token lives only inside one token mint and is never cached or logged.
//
// Four endpoints back the music module: "search" resolves free text to
// tracks, "track" and "artist" look up bare catalog ids, and "nowplaying"
// reads the broadcaster's currently-playing track. Search and the lookups
// ride any valid user token; nowplaying additionally requires
// user-read-currently-playing (or user-read-playback-state) on the
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

// Config carries the provider's environment: the two Spotify hosts, the
// fleet's one Spotify app credentials, and the per-broadcaster request
// ceiling. There is no broadcaster credential here — those are per caller,
// resolved just-in-time.
type Config struct {
	BaseURL      string
	AccountsURL  string
	ClientID     string
	ClientSecret string
	RateLimit    float64
}

// providerName is the subject token this provider answers under.
const providerName = "spotify"

// api holds the provider's runtime pieces; the declared endpoints capture it.
// Every endpoint stays a bespoke handler: they must resolve the broadcaster's
// credential (and possibly mint a token) before any cache or upstream work,
// which the shared byte-flow skeleton deliberately does not model — a cached
// reply must never bypass the credential checks.
type api struct {
	// http dials api.spotify.com. No baked auth header: the access token is
	// per broadcaster (see bearerHeader).
	http *core.HTTPClient
	// auth dials accounts.spotify.com for the token mint; the app credentials
	// ride the form body, not a baked header.
	auth    *core.HTTPClient
	cache   *core.Cache
	keys    provider.BroadcasterKeyResolver
	log     *zap.Logger
	limiter *ratelimit.Limiter

	// buckets is the per-broadcaster budget template: the derived specs are
	// computed once here and re-keyed per caller (see rateAdmit).
	buckets core.Buckets

	clientID     string
	clientSecret string
}

// New builds the spotify provider. d.SpotifyKeys and the app credentials must
// all be present (providers.All skips the provider otherwise, since with no
// resolver or no app to exchange against it can authenticate nothing).
func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	b.Endpoint("search").Timeout(lookupTimeout).Handle(p.search)
	b.Endpoint("track").Timeout(lookupTimeout).Handle(p.track)
	b.Endpoint("artist").Timeout(lookupTimeout).Handle(p.artist)
	b.Endpoint("nowplaying").Timeout(nowplayingTimeout).Handle(p.nowPlaying)
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
		http:         b.Client(base, nil, httpTimeout),
		auth:         b.Client(accounts, nil, httpTimeout),
		cache:        d.Cache,
		keys:         d.SpotifyKeys,
		log:          d.Logger(),
		limiter:      d.Limiter,
		buckets:      core.NewBuckets("ratelimit:gossip:spotify", cfg.RateLimit, rateWindowSeconds),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
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
func (p *api) accessToken(ctx context.Context, broadcaster, refreshToken string) (accessToken, error) {
	cacheKey := core.Key(providerName, "token", broadcaster)
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		tok, err := p.mintToken(ctx, broadcaster, refreshToken)
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
// 401 every call until the background refill lands — so the skew margin is
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
// using the fleet's own app credentials (confidential-client form flow).
//
// Spotify MAY rotate the refresh token on this exchange. Custody of the
// stored token belongs to the modules service, so the replacement is written
// back through its compare-and-swap rotate verb (see persistRotation) —
// best-effort, never blocking the mint that already succeeded.
func (p *api) mintToken(ctx context.Context, broadcaster, refreshToken string) (tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
	}
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
	p.persistRotation(ctx, broadcaster, refreshToken, tok.RefreshToken)
	return tok, nil
}

// accessTokenFor resolves the broadcaster's refresh token and returns a live
// access token, or ("", msg) with a reply-safe reason when it cannot.
func (p *api) accessTokenFor(ctx context.Context, broadcaster string) (tok accessToken, msg string) {
	refresh, err := p.keys.Key(ctx, broadcaster)
	if err != nil {
		p.log.Warn("spotify refresh-token resolve failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return "", "could not read your Spotify connection"
	}
	if refresh == "" {
		return "", "no Spotify connection on file"
	}
	tok, err = p.accessToken(ctx, broadcaster, refresh)
	if err != nil {
		p.log.Warn("spotify token mint failed", zap.String("broadcaster", broadcaster), zap.Error(err))
		return "", friendlyAuthError(err)
	}
	return tok, ""
}

// deadCredential reports the token-endpoint statuses that mean the stored
// grant itself is gone — revoked, expired, disconnected — rather than
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
// escaped — callers resolve URIs down to ids themselves.
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
// are the slim variant — names, artists, durations, links, but no album
// object and no artwork — so both are filled in from the album itself.
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
// window its cache entry — broadcaster scoping, result cap, canonical input
// identity, freshness — so the cache wrapper takes two arguments instead of
// six loose primitives.
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
// hit returns the final empty reply — no result is an answer, not an error.
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
// with no item — exactly the right shape for "nothing playing".
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

	// Unlike the catalog reads this endpoint is polled — chat asks many times
	// a minute against ONE broadcaster's token allowance — so its miss path
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
