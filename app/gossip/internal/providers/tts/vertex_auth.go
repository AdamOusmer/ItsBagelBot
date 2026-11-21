// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"ItsBagelBot/pkg/codec"
	"cmp"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
)

const (
	defaultTokenURI = "https://oauth2.googleapis.com/token"
	vertexScope     = "https://www.googleapis.com/auth/cloud-platform"
	jwtGrant        = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	refreshLead     = 5 * time.Minute
)

var (
	errInvalidServiceAccount = errors.New("invalid Vertex service-account JSON")
	errInvalidPrivateKey     = errors.New("Vertex service-account key must be PKCS8 RSA")
	errInvalidTokenURI       = errors.New("Vertex token URI must use a trusted Google OAuth host")
)

type serviceAccount struct {
	project  string
	email    string
	tokenURI *url.URL
	key      *rsa.PrivateKey
}

func parseServiceAccount(raw string) (serviceAccount, error) {
	var fields struct {
		Type       string `json:"type"`
		Project    string `json:"project_id"`
		PrivateKey string `json:"private_key"`
		Email      string `json:"client_email"`
		TokenURI   string `json:"token_uri"`
	}
	if err := codec.Unmarshal([]byte(raw), &fields); err != nil {
		return serviceAccount{}, errInvalidServiceAccount
	}
	if fields.Type != "service_account" || fields.Email == "" {
		return serviceAccount{}, errInvalidServiceAccount
	}
	tokenURI, err := url.Parse(cmp.Or(fields.TokenURI, defaultTokenURI))
	if err != nil || !trustedTokenURI(tokenURI) {
		return serviceAccount{}, errInvalidTokenURI
	}
	key, err := parsePrivateKey(fields.PrivateKey)
	if err != nil {
		return serviceAccount{}, err
	}
	return serviceAccount{project: fields.Project, email: fields.Email, tokenURI: tokenURI, key: key}, nil
}

func parsePrivateKey(text string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errInvalidPrivateKey
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	private, ok := key.(*rsa.PrivateKey)
	if err != nil || !ok {
		return nil, errInvalidPrivateKey
	}
	return private, nil
}

func trustedTokenURI(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil {
		return false
	}
	if port := u.Port(); port != "" && port != "443" {
		return false
	}
	if u.Fragment != "" {
		return false
	}
	switch u.Hostname() {
	case "oauth2.googleapis.com", "accounts.google.com", "www.googleapis.com":
		return true
	default:
		return false
	}
}

type vertexClaims struct {
	Issuer   string `json:"iss"`
	Scope    string `json:"scope"`
	Audience string `json:"aud"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

func (a serviceAccount) assertion(now time.Time) (string, error) {
	claims, err := codec.Marshal(vertexClaims{
		Issuer:   a.email,
		Scope:    vertexScope,
		Audience: a.tokenURI.String(),
		IssuedAt: now.Unix(),
		Expires:  now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", errUnavailable
	}
	encoding := base64.RawURLEncoding
	header := encoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := header + "." + encoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errUnavailable
	}
	return payload + "." + encoding.EncodeToString(signature), nil
}

type vertexAuth struct {
	client  *core.HTTPClient
	account serviceAccount

	mu      sync.Mutex
	cached  vertexAccessToken
	refresh *vertexTokenRefresh
}

type vertexAccessToken struct {
	value     string
	refreshAt time.Time
}

type vertexTokenRefresh struct {
	done  chan struct{}
	token vertexAccessToken
	err   error
}

func (a *vertexAuth) token(ctx context.Context) (string, error) {
	a.mu.Lock()
	if time.Now().Before(a.cached.refreshAt) {
		value := a.cached.value
		a.mu.Unlock()
		return value, nil
	}
	if current := a.refresh; current != nil {
		a.mu.Unlock()
		return current.wait(ctx)
	}
	refresh := &vertexTokenRefresh{done: make(chan struct{})}
	a.refresh = refresh
	a.mu.Unlock()

	refresh.token, refresh.err = a.exchange(ctx)
	a.mu.Lock()
	a.cached = refresh.token
	a.refresh = nil
	close(refresh.done)
	a.mu.Unlock()
	return refresh.token.value, refresh.err
}

func (r *vertexTokenRefresh) wait(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-r.done:
		return r.token.value, r.err
	}
}

func (a *vertexAuth) exchange(ctx context.Context) (vertexAccessToken, error) {
	now := time.Now()
	assertion, err := a.account.assertion(now)
	if err != nil {
		return vertexAccessToken{}, err
	}
	form := url.Values{"grant_type": {jwtGrant}, "assertion": {assertion}}
	var token struct {
		Value     string `json:"access_token"`
		ExpiresIn int64  `json:"expires_in"`
	}
	err = a.client.Do(ctx, core.Request{
		Method:      http.MethodPost,
		Path:        a.account.tokenURI.RequestURI(),
		Headers:     map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:        []byte(form.Encode()),
		NoRedirects: true,
		DecodeError: decodeVertexTokenError,
	}, &token)
	if err != nil {
		return vertexAccessToken{}, err
	}
	switch {
	case token.Value == "", token.ExpiresIn <= 0, token.ExpiresIn > math.MaxInt64/int64(time.Second):
		return vertexAccessToken{}, errUnavailable
	}
	return vertexAccessToken{
		value:     token.Value,
		refreshAt: now.Add(time.Duration(token.ExpiresIn)*time.Second - refreshLead),
	}, nil
}

func decodeVertexTokenError(upstream *core.UpstreamError, body []byte) error {
	switch {
	case upstream.Status == http.StatusBadRequest:
		return fmt.Errorf("%w: %w", errVertexCredentialsRejected, upstream)
	case upstream.Status == http.StatusUnauthorized, upstream.Status == http.StatusForbidden, upstream.Status == http.StatusTooManyRequests:
		return decodeVertexError(upstream, body)
	case upstream.Status > http.StatusBadRequest && upstream.Status < http.StatusInternalServerError:
		return errUnavailable
	default:
		return upstream
	}
}
