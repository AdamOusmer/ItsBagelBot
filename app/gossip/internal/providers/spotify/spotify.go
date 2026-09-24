// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	searchTTL     = 10 * time.Minute
	trackTTL      = time.Hour
	nowplayingTTL = 15 * time.Second
	negativeTTL   = 15 * time.Second

	httpTimeout       = 6 * time.Second
	lookupTimeout     = 8 * time.Second
	nowplayingTimeout = 8 * time.Second

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

	tokenExpirySkew = 60 * time.Second
	minTokenTTL     = 30 * time.Second
)

type Config struct {
	BaseURL     string
	AccountsURL string
	RateLimit   float64
}

const providerName = "spotify"

type api struct {
	http    *core.HTTPClient
	auth    *core.HTTPClient
	cache   *core.Cache
	keys    provider.SpotifyCredResolver
	log     *zap.Logger
	limiter *ratelimit.Limiter

	buckets core.Buckets
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(cfg, d, b)
	b.Endpoint("search").Timeout(lookupTimeout).Handle(p.search)
	b.Endpoint("track").Timeout(lookupTimeout).Handle(p.track)
	b.Endpoint("artist").Timeout(lookupTimeout).Handle(p.artist)
	b.Endpoint("nowplaying").Timeout(nowplayingTimeout).Handle(p.nowPlaying)
	b.Endpoint("exchange").Timeout(lookupTimeout).Handle(p.exchange)
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

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

type accessToken string

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

func tokenCacheTTL(expiresIn int) time.Duration {
	ttl := (time.Duration(expiresIn)*time.Second - tokenExpirySkew) / 2
	if ttl < minTokenTTL {
		ttl = minTokenTTL
	}
	return ttl
}

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

func (p *api) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	var tok tokenResponse
	req := core.Request{
		Method: http.MethodPost,
		Path:   tokenPath,
		Headers: map[string]string{
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

func deadCredential(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return true
	}
	return false
}

func friendlyAuthError(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) && deadCredential(ue.Status) {
		return "your Spotify connection needs to be set up again"
	}
	return "could not reach Spotify"
}

func bearerHeader(token accessToken) map[string]string {
	return map[string]string{"Authorization": bearerTokenType + " " + string(token)}
}

func (p *api) rateAdmit(broadcaster string) func(context.Context) error {
	return func(ctx context.Context) error {
		return p.buckets.WithKey("ratelimit:gossip:spotify:"+broadcaster).Enforce(ctx, p.limiter, true)
	}
}

func spotifyFriendlyError(err error) (string, core.Pin) {
	var ue *core.UpstreamError
	if !errors.As(err, &ue) {
		return "", core.PinNone
	}
	switch ue.Status {
	case http.StatusBadRequest:
		return "invalid request", core.PinNegative
	case http.StatusNotFound:
		return "not found on Spotify", core.PinNegative
	case http.StatusForbidden:
		return "Spotify playback not permitted right now", core.PinNone
	case http.StatusTooManyRequests:
		return spotifyThrottled(ue)
	case http.StatusServiceUnavailable:
		return "Spotify is unavailable right now, try again in a moment", core.PinThrottle
	}
	return "", core.PinNone
}

func spotifyThrottled(ue *core.UpstreamError) (string, core.Pin) {
	if ue.LocalDeny {
		return "Spotify is busy right now, try again in a few seconds", core.PinNone
	}
	return "Spotify is rate limiting requests right now, try again in a moment", core.PinThrottle
}

func (p *api) fetchFailed(what, fallback string, err error) string {
	if msg, _ := spotifyFriendlyError(err); msg != "" {
		return msg
	}
	p.log.Warn(what, zap.Error(err))
	return fallback
}

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

// Concatenated into the request path: anything outside [A-Za-z0-9] must be rejected.
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

type searchResponse struct {
	Tracks struct {
		Items []trackItem `json:"items"`
	} `json:"tracks"`
}

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

	if target.kind == resolveUnsupportedLink {
		return gossiprpc.SpotifySearchReply{Error: "that Spotify link type isn't supported; share a track or album"}
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
	case resolveAlbumID:
		scope.ttl = trackTTL
		return p.searchCached(ctx, scope, p.albumTracksFetch(tok, target.id, scope.limit))
	default:
		return p.searchCached(ctx, scope, p.textFetch(tok, rawQuery(req), scope.limit))
	}
}

func missingSearchInput(req gossiprpc.Request) string {
	if strings.TrimSpace(req.Query) == "" {
		return "missing search query"
	}
	if strings.TrimSpace(req.ChannelID) == "" {
		return "missing channel"
	}
	return ""
}

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

type searchScope struct {
	broadcaster string
	limit       int
	canonical   string
	ttl         time.Duration
}

type searchFetch func(context.Context) (gossiprpc.SpotifySearchReply, error)

func (p *api) searchCached(ctx context.Context, scope searchScope, fetch searchFetch) any {
	cacheKey := core.Key(providerName, "search",
		fmt.Sprintf("%s|%d|%s", scope.broadcaster, scope.limit, scope.canonical))
	b, err := core.CachedBytes(ctx, p.cache, cacheKey, nil, func(ctx context.Context) ([]byte, time.Duration, error) {
		b, ttl, _, err := core.BuildReplyWithMapper(ctx, scope.ttl, negativeTTL,
			func(ctx context.Context) (any, error) { return fetch(ctx) },
			func(msg string) any { return gossiprpc.SpotifySearchReply{Error: msg} },
			spotifyFriendlyError,
		)
		return b, ttl, err
	})
	if err != nil {
		return gossiprpc.SpotifySearchReply{Error: p.fetchFailed("spotify search fetch failed", "track search failed", err)}
	}
	return codec.RawMessage(b)
}

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

func (p *api) albumTracksFetch(tok accessToken, id string, limit int) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		return p.albumTracks(ctx, tok, id, limit)
	}
}

func (p *api) textFetch(tok accessToken, raw string, limit int) searchFetch {
	return func(ctx context.Context) (gossiprpc.SpotifySearchReply, error) {
		return p.searchText(ctx, tok, planTextSearch(raw), limit)
	}
}

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

func truncateTracks(reply gossiprpc.SpotifySearchReply, limit int) gossiprpc.SpotifySearchReply {
	if len(reply.Tracks) > limit {
		reply.Tracks = reply.Tracks[:limit]
	}
	return reply
}

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
		b, ttl, _, err := core.BuildReplyWithMapper(ctx, trackTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				var it trackItem
				req := core.Request{Method: http.MethodGet, Path: trackPath + id, Headers: bearerHeader(tok)}
				if err := p.http.Do(ctx, req, &it); err != nil {
					return nil, err
				}
				return gossiprpc.SpotifyTrackReply{Track: shapeTrack(it)}, nil
			},
			func(msg string) any { return gossiprpc.SpotifyTrackReply{Error: msg} },
			spotifyFriendlyError,
		)
		return b, ttl, err
	})
	if err != nil {
		return gossiprpc.SpotifyTrackReply{Error: p.fetchFailed("spotify track fetch failed", "track lookup failed", err)}
	}
	return codec.RawMessage(b)
}

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
		b, ttl, _, err := core.BuildReplyWithMapper(ctx, trackTTL, negativeTTL,
			func(ctx context.Context) (any, error) {
				var resp artistResponse
				req := core.Request{Method: http.MethodGet, Path: artistPath + id, Headers: bearerHeader(tok)}
				if err := p.http.Do(ctx, req, &resp); err != nil {
					return nil, err
				}
				return gossiprpc.SpotifyArtistReply{Artist: shapeArtist(resp.ID, resp.artistItem)}, nil
			},
			func(msg string) any { return gossiprpc.SpotifyArtistReply{Error: msg} },
			spotifyFriendlyError,
		)
		return b, ttl, err
	})
	if err != nil {
		return gossiprpc.SpotifyArtistReply{Error: p.fetchFailed("spotify artist fetch failed", "artist lookup failed", err)}
	}
	return codec.RawMessage(b)
}

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

	b, err := core.CachedBytes(ctx, p.cache, core.Key(providerName, "nowplaying", broadcaster), p.rateAdmit(broadcaster),
		func(ctx context.Context) ([]byte, time.Duration, error) {
			b, ttl, _, err := core.BuildReplyWithMapper(ctx, nowplayingTTL, negativeTTL,
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
				spotifyFriendlyError,
			)
			return b, ttl, err
		})
	if err != nil {
		return gossiprpc.SpotifyNowPlayingReply{Error: p.fetchFailed("spotify now-playing fetch failed", "could not reach Spotify", err)}
	}
	return codec.RawMessage(b)
}

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

	return gossiprpc.SpotifyExchangeReply{RefreshToken: tok.RefreshToken, Scopes: splitScopes(tok.Scope)}
}

func splitScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' })
	if len(fields) == 0 {
		return nil
	}
	return fields
}

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

func (p *api) playerFailed(err error) string {
	var ue *core.UpstreamError
	if errors.As(err, &ue) {
		switch ue.Status {
		case http.StatusNotFound:
			return "no active Spotify device, start playing something first"
		case http.StatusForbidden:
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

func (p *api) next(ctx context.Context, req gossiprpc.Request) any {
	return p.playerWrite(ctx, req, func(ctx context.Context, tok accessToken) error {
		return p.http.Do(ctx, core.Request{Method: http.MethodPost, Path: nextPath, Headers: bearerHeader(tok)}, nil)
	})
}
