// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package youtube

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// refreshMargin renews tokens this long before Google would reject them, so
// in-flight requests never race expiry. Google access tokens live ~1h.
const refreshMargin = 5 * time.Minute

// tokenRequestTimeout bounds one refresh grant POST end to end. Same shape as
// the Twitch client's bound: comfortably above a cold TLS round trip, short
// enough that a hung grant fails before the lane's pacing matters.
const tokenRequestTimeout = 5 * time.Second

// TokenURL is Google's OAuth 2.0 token endpoint; a var so tests can point it
// at an httptest server.
var TokenURL = "https://oauth2.googleapis.com/token"

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	Error       struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func postRefreshGrant(ctx context.Context, clientID, clientSecret, refreshToken string) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenRequestTimeout)
	defer cancel()

	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	if err != nil {
		return "", 0, err
	}

	var parsed tokenResponse
	if err := codec.Unmarshal(raw, &parsed); err != nil {
		return "", 0, fmt.Errorf("youtube: malformed token response (%d): %s", res.StatusCode, raw)
	}
	if res.StatusCode != http.StatusOK || parsed.AccessToken == "" {
		return "", 0, fmt.Errorf("youtube: refresh grant rejected (%d): %s",
			res.StatusCode, strings.TrimSpace(string(raw)))
	}

	ttl := time.Duration(parsed.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour // Google's default when expires_in is absent
	}
	return parsed.AccessToken, ttl, nil
}

// BotTokenSource caches one bot-identity access token minted from the seeded
// refresh-token grant. Google does not rotate refresh tokens by default, so —
// unlike the Twitch source — no rotation bookkeeping is needed; the lease
// fronting this source (wired via MintLease) exists only to keep three
// replicas from stampeding the grant endpoint at the same expiry moment.
//
// Invalidate forgets the cached token after a 401 so the caller's single
// fresh-token retry mints anew.
type BotTokenSource struct {
	clientID     string
	clientSecret string
	refreshToken string

	mu      sync.RWMutex
	token   string
	expires time.Time
	group   singleflight.Group
}

func NewBotTokenSource(clientID, clientSecret, refreshToken string) *BotTokenSource {
	return &BotTokenSource{
		clientID:     clientID,
		clientSecret: clientSecret,
		refreshToken: refreshToken,
	}
}

func (s *BotTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.RLock()
	cached, ok := s.token, time.Now().Add(refreshMargin).Before(s.expires)
	s.mu.RUnlock()
	if ok && cached != "" {
		return cached, nil
	}

	v, err, _ := s.group.Do("token", func() (any, error) {
		token, ttl, err := postRefreshGrant(ctx, s.clientID, s.clientSecret, s.refreshToken)
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		s.token, s.expires = token, time.Now().Add(ttl)
		s.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// Invalidate drops the cached token; the next Token call mints afresh.
func (s *BotTokenSource) Invalidate() {
	s.mu.Lock()
	s.token = ""
	s.mu.Unlock()
}

// LeasedTokens resolves short-lived per-channel access tokens over NATS RPC
// from the users service — the same contract the ingress consumes
// (`bagel.rpc.youtube.token.get`, {"channel_id"} -> {"access_token",
// "expires_at"}). Entries cache until their expiry minus margin; a lookup
// failure is returned, never swallowed, because sending with no credential
// would only earn a 401 downstream anyway.
type LeasedTokens struct {
	request func(ctx context.Context, channelID string) (string, time.Time, error)

	mu      sync.Mutex
	entries map[string]leasedToken
}

type leasedToken struct {
	token   string
	expires time.Time
}

func NewLeasedTokens(request func(ctx context.Context, channelID string) (string, time.Time, error)) *LeasedTokens {
	return &LeasedTokens{request: request, entries: map[string]leasedToken{}}
}

func (l *LeasedTokens) Token(ctx context.Context, channelID string) (string, error) {
	l.mu.Lock()
	entry, ok := l.entries[channelID]
	l.mu.Unlock()

	if ok && time.Now().Add(refreshMargin).Before(entry.expires) {
		return entry.token, nil
	}

	token, expires, err := l.request(ctx, channelID)
	if err != nil {
		return "", err
	}

	l.mu.Lock()
	l.entries[channelID] = leasedToken{token: token, expires: expires}
	l.mu.Unlock()
	return token, nil
}
