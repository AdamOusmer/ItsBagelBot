package twitch

import (
	"context"
	"encoding/json"
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

// ClientCredentials is the Twitch application's client id + secret, presented
// on every OAuth token grant (client-credentials and refresh-token alike).
type ClientCredentials struct {
	ID     string
	Secret string
}

// appGrant builds the client-credentials form (app access token).
func (c ClientCredentials) appGrant() url.Values {
	return url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"grant_type":    {"client_credentials"},
	}
}

// refreshGrant builds the refresh-token form (user access token) for one
// refresh token.
func (c ClientCredentials) refreshGrant(refreshToken string) url.Values {
	return url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
}

// NewAppTokenSource mints app access tokens through the client credentials
// grant. App tokens authorize most Helix endpoints the bot calls.
func NewAppTokenSource(creds ClientCredentials) *Source {

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, creds.appGrant())
		if err != nil {
			return "", 0, err
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

// NewStaticTokenSource returns a Source that always yields token without any
// network. It exists for tests and benchmarks that must run the full request
// path offline; production wiring never uses it.
func NewStaticTokenSource(token string) *Source {
	return &Source{refresh: func(context.Context) (string, time.Duration, error) {
		return token, 24 * time.Hour, nil
	}}
}

// NewUserTokenSource mints user access tokens for the bot account through
// the refresh token grant. Twitch may rotate the refresh token on every
// renewal, so the latest one is kept in memory.
func NewUserTokenSource(creds ClientCredentials, refreshToken string) *Source {

	current := refreshToken

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, creds.refreshGrant(current))
		if err != nil {
			return "", 0, err
		}

		if res.RefreshToken != "" {
			current = res.RefreshToken
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

// StoredTokenIO wires a stored user token to the users service: load runs
// before every renewal (so a token the operator installs through the admin
// panel takes effect without a restart), and persist runs after every rotation
// (so a restart never resurrects a stale refresh token). load returning "" means
// "keep what you have".
type StoredTokenIO struct {
	Load    func(ctx context.Context) string
	Persist func(ctx context.Context, accessToken, refreshToken string)
}

// NewStoredUserTokenSource works like NewUserTokenSource but sources the
// refresh token from the users service instead of the environment.
// fallbackRefresh seeds the chain while the store is empty.
func NewStoredUserTokenSource(creds ClientCredentials, fallbackRefresh string, io StoredTokenIO) *Source {

	current := fallbackRefresh

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		if stored := io.Load(ctx); stored != "" {
			current = stored
		}
		if current == "" {
			return "", 0, ErrNoRefreshToken
		}

		res, err := postToken(ctx, creds.refreshGrant(current))
		if err != nil {
			return "", 0, err
		}

		if res.RefreshToken != "" {
			current = res.RefreshToken
		}
		io.Persist(ctx, res.AccessToken, current)

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
		return oauthResponse{}, &TokenError{Status: res.StatusCode, Body: string(body)}
	}

	var parsed oauthResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return oauthResponse{}, err
	}

	return parsed, nil
}
