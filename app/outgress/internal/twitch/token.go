package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const tokenEndpoint = "https://id.twitch.tv/oauth2/token"

// refreshMargin renews tokens this long before Twitch would reject them, so
// in-flight requests never race the expiry.
const refreshMargin = 5 * time.Minute

var tokenHTTP = &http.Client{Timeout: 10 * time.Second}

// Source caches one OAuth access token and refreshes it before expiry or
// after Invalidate. All methods are safe for concurrent use.
type Source struct {
	mu      sync.RWMutex
	token   string
	expires time.Time
	refresh func(ctx context.Context) (string, time.Duration, error)
	group   singleflight.Group
}

// NewAppTokenSource mints app access tokens through the client credentials
// grant. App tokens authorize most Helix endpoints the bot calls.
func NewAppTokenSource(clientID, clientSecret string) *Source {

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, url.Values{
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"grant_type":    {"client_credentials"},
		})
		if err != nil {
			return "", 0, err
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

// NewUserTokenSource mints user access tokens for the bot account through
// the refresh token grant. Twitch may rotate the refresh token on every
// renewal, so the latest one is kept in memory.
func NewUserTokenSource(clientID, clientSecret, refreshToken string) *Source {

	current := refreshToken

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, url.Values{
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"grant_type":    {"refresh_token"},
			"refresh_token": {current},
		})
		if err != nil {
			return "", 0, err
		}

		if res.RefreshToken != "" {
			current = res.RefreshToken
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

// NewStoredUserTokenSource works like NewUserTokenSource but sources the
// refresh token from the users service instead of the environment: load runs
// before every renewal, so a token the operator installs through the admin
// panel takes effect without a restart, and persist runs after every
// rotation, so a restart never resurrects a stale refresh token.
// fallbackRefresh seeds the chain while the store is empty; load returning
// "" means "keep what you have".
func NewStoredUserTokenSource(clientID, clientSecret, fallbackRefresh string,
	load func(ctx context.Context) string,
	persist func(ctx context.Context, accessToken, refreshToken string)) *Source {

	current := fallbackRefresh

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		if stored := load(ctx); stored != "" {
			current = stored
		}
		if current == "" {
			return "", 0, errors.New("no bot refresh token available")
		}

		res, err := postToken(ctx, url.Values{
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"grant_type":    {"refresh_token"},
			"refresh_token": {current},
		})
		if err != nil {
			return "", 0, err
		}

		if res.RefreshToken != "" {
			current = res.RefreshToken
		}
		persist(ctx, res.AccessToken, current)

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

// Token returns a valid access token, renewing it when missing or close to
// expiry. When renewal fails but the cached token is still within its
// lifetime, the cached token is returned so a transient id.twitch.tv outage
// does not take the egress path down with it.
func (s *Source) Token(ctx context.Context) (string, error) {
	if token, ok := s.cached(refreshMargin); ok {
		return token, nil
	}

	value, err, _ := s.group.Do("refresh", func() (any, error) {
		// Another caller may have completed the refresh while this caller waited.
		if token, ok := s.cached(refreshMargin); ok {
			return token, nil
		}

		// refresh performs NATS RPC and HTTP I/O. It intentionally runs outside
		// mu so status calls and invalidation never queue behind a slow network
		// operation; singleflight still guarantees one refresh per Source.
		token, ttl, err := s.refresh(ctx)
		if err != nil {
			if cached, ok := s.cached(0); ok {
				return cached, nil
			}
			return "", err
		}

		s.mu.Lock()
		s.token = token
		s.expires = time.Now().Add(ttl)
		s.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (s *Source) cached(margin time.Duration) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token, s.token != "" && time.Until(s.expires) > margin
}

// Invalidate discards the cached token so the next Token call renews it.
// Called after a 401, which means Twitch revoked the token early.
func (s *Source) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = ""
}

// ExpiresIn reports the remaining lifetime of the cached token, zero when
// none is held. Exposed through the system status RPC.
func (s *Source) ExpiresIn() time.Duration {

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.token == "" {
		return 0
	}

	remaining := time.Until(s.expires)
	if remaining < 0 {
		return 0
	}

	return remaining
}

type oauthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func postToken(ctx context.Context, form url.Values) (oauthResponse, error) {

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := tokenHTTP.Do(req)
	if err != nil {
		return oauthResponse{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return oauthResponse{}, fmt.Errorf("token request failed: %d %s", res.StatusCode, string(body))
	}

	var parsed oauthResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return oauthResponse{}, err
	}

	return parsed, nil
}
