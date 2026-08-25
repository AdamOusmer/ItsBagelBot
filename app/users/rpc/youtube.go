// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// GoogleTokenURL is Google's OAuth 2.0 token endpoint; a var so tests can
// point it at an httptest server.
var GoogleTokenURL = "https://oauth2.googleapis.com/token"

const (
	// googleGrantTimeout bounds one refresh-grant POST end to end. Same shape
	// as the Twitch client's bound: comfortably above a cold TLS round trip,
	// short enough that a hung grant fails before the caller's own deadline.
	googleGrantTimeout = 4 * time.Second

	// googleMaxErrorBody caps how much of an error body is read/logged;
	// Google error payloads are small JSON, anything longer is noise.
	googleMaxErrorBody = 8 << 10

	// youtubeLeaseTimeout bounds one lease request's whole handler, mint
	// included. yt-ingress waits ~2s (YT_TOKEN_TIMEOUT_MS); a larger server
	// budget lets a slow mint finish and cache its result so the caller's
	// retry succeeds instantly instead of racing another mint.
	youtubeLeaseTimeout = 5 * time.Second

	// youtubeServeMargin refuses to hand out a stored access token this close
	// to its expiry, preferring a fresh mint. Deliberately wider than
	// yt-ingress's own 60s refresh margin (NATS_YT_TOKEN_REFRESH_MARGIN_
	// SECONDS): if we serve a token with less than their margin left, their
	// cache logic immediately re-requests it -- paying a second lease round
	// trip for nothing. Clock skew between pods is bounded by NTP (tens of
	// ms), well inside the gap between the two margins.
	youtubeServeMargin = 2 * time.Minute
)

// GoogleCredentials carries the OAuth client this service mints YouTube
// access tokens with. Empty means "youtube linking not provisioned": grants
// already stored stay readable, but nothing can be minted until configured.
type GoogleCredentials struct {
	ClientID     string
	ClientSecret string
}

// Configured reports whether a refresh grant is possible at all.
func (c GoogleCredentials) Configured() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

// youtubeTokens serves bagel.rpc.youtube.token.get: per-channel short-lived
// Google access tokens leased out of the users service's stored grants.
//
// Roles mirror the Twitch surface exactly, with one inversion: Twitch token
// renewal lives in outgress (it holds the bot credential), while here the
// users service itself mints, because the ingress never stores refresh
// tokens and there is no other holder (ADR 0011). Plaintext transits only
// these subjects; NATS authorization restricts who may subscribe.
type youtubeTokens struct {
	repo   *repository.Users
	creds  GoogleCredentials
	log    *zap.Logger
	group  singleflight.Group // one in-flight mint per channel id
	client *http.Client
}

func newYouTubeTokens(repo *repository.Users, creds GoogleCredentials, log *zap.Logger) *youtubeTokens {
	return &youtubeTokens{
		repo:   repo,
		creds:  creds,
		log:    log,
		client: http.DefaultClient,
	}
}

// SubscribeYouTubeTokens binds the lease subject. prefix is the FULL subject
// (bagel.rpc.youtube.token.get) because the contract has exactly one verb --
// unlike SubscribeTokens, whose prefix+verb shape exists for get/save pairs.
func SubscribeYouTubeTokens(w Wiring, prefix string, creds GoogleCredentials) error {
	y := newYouTubeTokens(w.Repo, creds, w.Log)
	return bus.QueueSubscribeJSON[usersrpc.YouTubeTokenGetRequest, usersrpc.YouTubeTokenGetReply](
		w.NC,
		prefix,
		w.Queue,
		youtubeLeaseTimeout,
		w.App,
		w.Log,
		y.handleGet,
	)
}

// handleGet answers {"channel_id"} -> {"channel_id","access_token","expires_at"}
// (unix seconds), the exact shape pinned by
// app/yt-ingress/lib/yt_ingress/token_source.ex:
//
//	request:  {"channel_id": "UC_x5XG1OV2P6uZZ5FSM9Ttw"}
//	reply:    {"channel_id": "...", "access_token": "...", "expires_at": 1755880000}
//
// The Elixir decode pattern requires "access_token" and "expires_at" on any
// successful reply, so every failure path returns Error set and leaves those
// fields zero rather than answering with partial data.
//
// singleflight keys by channel id: three users replicas hit at once when a
// live stream starts (ingress discovery fires on all watchers), and each
// mint burns nothing but latency -- collapsing them keeps one POST to
// Google instead of three identical ones.
func (y *youtubeTokens) handleGet(ctx context.Context, req usersrpc.YouTubeTokenGetRequest) usersrpc.YouTubeTokenGetReply {
	if err := repository.ValidateYouTubeChannelID(req.ChannelID); err != nil {
		return usersrpc.YouTubeTokenGetReply{Error: "bad channel_id"}
	}

	v, err, _ := y.group.Do(req.ChannelID, func() (any, error) {
		return y.lease(ctx, req.ChannelID)
	})
	if err != nil {
		return usersrpc.YouTubeTokenGetReply{
			ChannelID: req.ChannelID,
			Error:     err.Error(),
		}
	}
	return v.(usersrpc.YouTubeTokenGetReply)
}

// lease resolves one channel's credential: serve the stored access token
// while it is comfortably valid, otherwise mint from the stored refresh
// token and persist the result so later leases skip the network.
func (y *youtubeTokens) lease(ctx context.Context, channelID string) (usersrpc.YouTubeTokenGetReply, error) {
	userID, access, refresh, expiresAt, err := y.repo.TokenByYouTubeChannel(ctx, channelID)
	if err != nil {
		return usersrpc.YouTubeTokenGetReply{}, fmt.Errorf("no youtube grant for channel")
	}

	now := time.Now()
	if len(access) > 0 && expiresAt != nil && expiresAt.Sub(now) > youtubeServeMargin {
		return usersrpc.YouTubeTokenGetReply{
			ChannelID:   channelID,
			AccessToken: string(access),
			ExpiresAt:   expiresAt.Unix(),
		}, nil
	}

	if !y.creds.Configured() {
		return usersrpc.YouTubeTokenGetReply{}, fmt.Errorf("youtube oauth not configured")
	}
	if len(refresh) == 0 {
		return usersrpc.YouTubeTokenGetReply{}, fmt.Errorf("grant has no refresh token")
	}

	minted, ttl, err := y.refreshGrant(ctx, string(refresh))
	if err != nil {
		return usersrpc.YouTubeTokenGetReply{}, err
	}

	expires := now.Add(ttl)
	// Persist the rotated access token so replicas and later leases reuse it
	// until expiry. The refresh token is passed back unchanged: Google does
	// not rotate refresh tokens on refresh grants (same reason outgress's
	// BotTokenSource needs no rotation bookkeeping).
	if err := y.repo.UpsertToken(ctx, userID, tokens.TypeUserToken, tokens.PlatformYoutube,
		minted, refresh, &expires); err != nil {
		// Non-fatal: the caller still gets a usable token; the next lease
		// just pays another mint. Never fail a live stream over bookkeeping.
		y.log.Warn("youtube lease persist failed",
			zap.String("channel_id", channelID), zap.Error(err))
	}

	return usersrpc.YouTubeTokenGetReply{
		ChannelID:   channelID,
		AccessToken: string(minted),
		ExpiresAt:   expires.Unix(),
	}, nil
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	Error       *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// refreshGrant exchanges a Google refresh token for a fresh access token
// (grant_type=refresh_token; PKCE/code_verifier/redirect_uri apply only to
// the authorization-code exchange, which stays in the console exactly where
// Twitch's exchange lives today).
//
// Deliberately a local copy of app/outgress/internal/youtube/token.go's
// postRefreshGrant rather than a shared package: app/outgress is another
// service's internals, and a pkg/ extraction would couple two services'
// release cadence over forty lines that have been stable since day one.
func (y *youtubeTokens) refreshGrant(ctx context.Context, refreshToken string) ([]byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, googleGrantTimeout)
	defer cancel()

	form := url.Values{
		"client_id":     {y.creds.ClientID},
		"client_secret": {y.creds.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GoogleTokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := y.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("google refresh grant: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, googleMaxErrorBody))
	if err != nil {
		return nil, 0, fmt.Errorf("google refresh grant: read body: %w", err)
	}

	var parsed googleTokenResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, 0, fmt.Errorf("google refresh grant: malformed response (%d)", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK || parsed.AccessToken == "" {
		msg := strings.TrimSpace(string(raw))
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return nil, 0, fmt.Errorf("google refresh grant rejected (%d): %s", res.StatusCode, msg)
	}

	ttl := time.Duration(parsed.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour // Google's default when expires_in is absent
	}
	return []byte(parsed.AccessToken), ttl, nil
}
