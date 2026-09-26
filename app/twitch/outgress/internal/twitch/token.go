// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const tokenEndpoint = "https://id.twitch.tv/oauth2/token"

const refreshMargin = 5 * time.Minute

const (
	tokenMaxIdleConnsPerHost = 2

	tokenIdleConnTimeout = 4*time.Hour + 30*time.Minute
)

var tokenHTTP = newTokenHTTPClient()

const tokenClientTimeout = 5 * time.Second

const (
	tokenH2ReadIdleTimeout = 15 * time.Second
	tokenH2PingTimeout     = 3 * time.Second
)

func newTokenHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = tokenMaxIdleConnsPerHost
	transport.IdleConnTimeout = tokenIdleConnTimeout
	transport.ForceAttemptHTTP2 = true
	transport.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: tokenH2ReadIdleTimeout,
		PingTimeout:     tokenH2PingTimeout,
	}
	return &http.Client{Transport: transport, Timeout: tokenClientTimeout}
}

func MaxMintLeaseHold() time.Duration {
	return tokenClientTimeout + persistTimeout
}

type Source struct {
	mu      sync.RWMutex
	token   string
	expires time.Time
	refresh func(ctx context.Context) (string, time.Duration, error)
	group   singleflight.Group
	gen     uint64

	skipAdopt  bool
	invalidTok string

	currentRefresh string
}

func (s *Source) getCurrentRefresh() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentRefresh
}

// Unconditional: Twitch already rotated the token, so every generation must present the new one.
func (s *Source) setCurrentRefresh(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentRefresh = v
}

type ClientCredentials struct {
	ID     string
	Secret string
}

func (c ClientCredentials) appGrant() url.Values {
	return url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"grant_type":    {"client_credentials"},
	}
}

func (c ClientCredentials) refreshGrant(refreshToken string) url.Values {
	return url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
}

func NewAppTokenSource(creds ClientCredentials) *Source {

	return &Source{refresh: func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, creds.appGrant())
		if err != nil {
			return "", 0, err
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}}
}

func NewStaticTokenSource(token string) *Source {
	return &Source{refresh: func(context.Context) (string, time.Duration, error) {
		return token, 24 * time.Hour, nil
	}}
}

func NewUserTokenSource(creds ClientCredentials, refreshToken string) *Source {

	s := &Source{currentRefresh: refreshToken}
	s.refresh = func(ctx context.Context) (string, time.Duration, error) {

		res, err := postToken(ctx, creds.refreshGrant(s.getCurrentRefresh()))
		if err != nil {
			return "", 0, err
		}

		if res.RefreshToken != "" {
			s.setCurrentRefresh(res.RefreshToken)
		}

		return res.AccessToken, time.Duration(res.ExpiresIn) * time.Second, nil
	}
	return s
}

type StoredLoad struct {
	RefreshToken         string
	AccessToken          string
	AccessTokenExpiresAt *time.Time
}

type StoredTokenIO struct {
	Load    func(ctx context.Context) StoredLoad
	Persist func(ctx context.Context, accessToken, refreshToken string, expiresAt time.Time) error
}

type MintLease struct {
	Acquire func(ctx context.Context) (release func(), ok bool, unavailable bool)
}

func NewStoredUserTokenSource(creds ClientCredentials, fallbackRefresh string, io StoredTokenIO, lease MintLease) *Source {

	s := &Source{currentRefresh: fallbackRefresh}

	m := minter{creds: creds, s: s, io: io, lease: lease}

	s.refresh = func(ctx context.Context) (token string, ttl time.Duration, err error) {

		stored := io.Load(ctx)
		if stored.RefreshToken != "" {
			s.setCurrentRefresh(stored.RefreshToken)
		}

		forbid := ""
		skip, badToken := s.consumeSkipAdopt()
		if skip {
			forbid = badToken
		}

		defer func() {
			if err != nil && skip {
				s.rearmSkipAdopt(badToken)
			}
		}()

		if token, ttl, ok := adoptCandidate(stored, forbid); ok {
			return token, ttl, nil
		}

		if s.getCurrentRefresh() == "" {
			return "", 0, ErrNoRefreshToken
		}

		return m.mintOrAdopt(ctx, forbid)
	}
	return s
}

type minter struct {
	creds ClientCredentials
	s     *Source
	io    StoredTokenIO
	lease MintLease
}

func (s *Source) rearmSkipAdopt(badToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skipAdopt {
		return
	}
	s.skipAdopt = true
	s.invalidTok = badToken
}

func adoptCandidate(stored StoredLoad, forbid string) (string, time.Duration, bool) {
	token, ttl, ok := adoptableStored(stored)
	if !ok || token == forbid {
		return "", 0, false
	}
	return token, ttl, true
}

const (
	leaseWaitInterval = 200 * time.Millisecond
	leaseWaitAttempts = 10
)

func (m minter) mintOnce(ctx context.Context) (oauthResponse, time.Duration, error) {
	res, err := postToken(ctx, m.creds.refreshGrant(m.s.getCurrentRefresh()))
	if err != nil {
		return oauthResponse{}, 0, err
	}
	if res.RefreshToken != "" {
		m.s.setCurrentRefresh(res.RefreshToken)
	}
	return res, time.Duration(res.ExpiresIn) * time.Second, nil
}

func (m minter) mintAndPersistAsync(ctx context.Context) (string, time.Duration, error) {
	res, ttl, err := m.mintOnce(ctx)
	if err != nil {
		return "", 0, err
	}
	m.persistAsync(res.AccessToken, m.s.getCurrentRefresh(), time.Now().Add(ttl))
	return res.AccessToken, ttl, nil
}

func (m minter) mintOrAdopt(ctx context.Context, forbid string) (string, time.Duration, error) {
	if m.lease.Acquire == nil {
		return m.mintAndPersistAsync(ctx)
	}

	release, ok, unavailable := m.lease.Acquire(ctx)
	if ok {
		return m.mintLeased(ctx, release)
	}
	if unavailable {
		return m.mintAndPersistAsync(ctx)
	}

	if token, ttl, done, err := m.waitForLeaseOrAdoption(ctx, forbid); done {
		return token, ttl, err
	}
	return m.mintAndPersistAsync(ctx)
}

// Releases after the first Persist attempt: holding through retries can outlive mintLeaseTTL.
func (m minter) mintLeased(ctx context.Context, release func()) (string, time.Duration, error) {
	res, ttl, err := m.mintOnce(ctx)
	if err != nil {
		release()
		return "", 0, err
	}
	m.persistAsyncThen(res.AccessToken, m.s.getCurrentRefresh(), time.Now().Add(ttl), release)
	return res.AccessToken, ttl, nil
}

func (m minter) waitForLeaseOrAdoption(ctx context.Context, forbid string) (token string, ttl time.Duration, done bool, err error) {
	for attempt := 0; attempt < leaseWaitAttempts; attempt++ {
		if attempt > 0 && !waitTick(ctx) {
			return "", 0, false, nil
		}
		if token, ttl, done, stop, err := m.pollLeaseOrAdoption(ctx, forbid); done || stop {
			return token, ttl, done, err
		}
	}
	return "", 0, false, nil
}

func waitTick(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(leaseWaitInterval):
		return true
	}
}

func (m minter) pollLeaseOrAdoption(ctx context.Context, forbid string) (token string, ttl time.Duration, done bool, stop bool, err error) {
	release, ok, unavailable := m.lease.Acquire(ctx)
	if ok {
		token, ttl, err := m.mintLeased(ctx, release)
		return token, ttl, true, false, err
	}
	if unavailable {
		return "", 0, false, true, nil
	}
	if token, ttl, ok := adoptCandidate(m.io.Load(ctx), forbid); ok {
		return token, ttl, true, false, nil
	}
	return "", 0, false, false, nil
}

func adoptableStored(stored StoredLoad) (string, time.Duration, bool) {
	if stored.AccessToken == "" || stored.AccessTokenExpiresAt == nil {
		return "", 0, false
	}
	ttl := time.Until(*stored.AccessTokenExpiresAt)
	if ttl <= refreshMargin {
		return "", 0, false
	}
	return stored.AccessToken, ttl, true
}

const (
	persistTimeout = 5 * time.Second

	persistAttempts     = 3
	persistRetryBackoff = 200 * time.Millisecond
)

func (m minter) persistAsync(accessToken, refreshToken string, expiresAt time.Time) {
	m.persistAsyncThen(accessToken, refreshToken, expiresAt, nil)
}

func (m minter) persistAsyncThen(accessToken, refreshToken string, expiresAt time.Time, onDone func()) {
	go func() {
		for attempt := 0; attempt < persistAttempts; attempt++ {
			if attempt > 0 {
				time.Sleep(persistRetryBackoff)
			}
			ok := persistOnce(m.io.Persist, accessToken, refreshToken, expiresAt)
			if attempt == 0 && onDone != nil {
				onDone()
			}
			if ok {
				return
			}
		}
	}()
}

func persistOnce(persist func(ctx context.Context, accessToken, refreshToken string, expiresAt time.Time) error, accessToken, refreshToken string, expiresAt time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	return persist(ctx, accessToken, refreshToken, expiresAt) == nil
}

func (s *Source) Token(ctx context.Context) (string, error) {
	if token, ok := s.cached(refreshMargin); ok {
		return token, nil
	}
	return s.singleflightRefresh(ctx)
}

const maxRefreshGenRetries = 1

func (s *Source) singleflightRefresh(ctx context.Context) (string, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		token, stale, err := s.singleflightRefreshOnce(ctx)
		if !stale || attempt >= maxRefreshGenRetries {
			return token, err
		}
		// stale: retry once under the new generation, which re-consumes the
		// (now armed) skip flag and carries the correct forbid value -- see
		// consumeSkipAdopt.
	}
}

func (s *Source) singleflightRefreshOnce(ctx context.Context) (token string, stale bool, err error) {
	s.mu.RLock()
	gen := s.gen
	s.mu.RUnlock()
	key := "refresh-" + strconv.FormatUint(gen, 10)

	result := s.group.DoChan(key, func() (any, error) {
		// Another caller may have completed the refresh while this caller waited.
		if token, ok := s.cached(refreshMargin); ok {
			return refreshResult{token: token}, nil
		}

		// refresh performs NATS RPC and HTTP I/O. It intentionally runs outside
		// mu so status calls and invalidation never queue behind a slow network
		// operation; singleflight still guarantees one refresh per Source.
		token, ttl, err := s.refresh(ctx)
		if err != nil {
			if cached, ok := s.cached(0); ok {
				return refreshResult{token: cached}, nil
			}
			return nil, err
		}

		if !s.storeIfGen(gen, token, ttl) {
			return refreshResult{stale: true}, nil
		}
		return refreshResult{token: token}, nil
	})
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case value := <-result:
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		if value.Err != nil {
			return "", false, value.Err
		}
		r := value.Val.(refreshResult)
		return r.token, r.stale, nil
	}
}

type refreshResult struct {
	token string
	stale bool
}

func (s *Source) storeIfGen(gen uint64, token string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return false
	}
	s.token = token
	s.expires = time.Now().Add(ttl)
	return true
}

const backgroundRefreshInterval = time.Minute

func (s *Source) StartBackgroundRefresh(ctx context.Context) {
	go s.runBackgroundRefresh(ctx)
}

func (s *Source) runBackgroundRefresh(ctx context.Context) {
	ticker := time.NewTicker(backgroundRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshIfDue(ctx)
		}
	}
}

func (s *Source) refreshIfDue(ctx context.Context) {
	if !s.refreshable(time.Now()) {
		return
	}
	if _, ok := s.cached(refreshMargin); ok {
		return
	}
	_, _ = s.singleflightRefresh(ctx)
}

func (s *Source) refreshable(now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token != "" && now.Before(s.expires)
}

func (s *Source) cached(margin time.Duration) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token, s.token != "" && time.Until(s.expires) > margin
}

func (s *Source) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidTok = s.token
	s.skipAdopt = true
	s.token = ""
	s.gen++
}

func (s *Source) consumeSkipAdopt() (skip bool, badToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	skip, badToken = s.skipAdopt, s.invalidTok
	s.skipAdopt = false
	s.invalidTok = ""
	return skip, badToken
}

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
	if err := codec.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return oauthResponse{}, err
	}

	return parsed, nil
}
