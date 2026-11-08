// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package oauth owns native device pairing and credential lifetime. Browser
// consent, native tokens and event transport have independent boundaries.
package oauth

import (
	"context"
	"crypto/tls"
	"errors"
	"net/url"
	"strings"
)

const ClientID = "itsbagelbot-desktop"
const AuthorizePath = "/oauth/desktop/authorize"
const TokenPath = "/oauth/desktop/token"
const RevokePath = "/oauth/desktop/revoke"

var (
	ErrConfiguration  = errors.New("oauth: native authorization configuration unavailable")
	ErrStorage        = errors.New("oauth: protected credentials unavailable")
	ErrAuthentication = errors.New("oauth: device authorization required")
	ErrUnavailable    = errors.New("oauth: authorization service unavailable")
	ErrProtocol       = errors.New("oauth: authorization response rejected")
	ErrBusy           = errors.New("oauth: device approval already pending")
	ErrStopped        = errors.New("oauth: native authorization stopped")
	ErrNotFound       = errors.New("oauth: no protected credentials")
)

// Store saves the whole credential family as one protected native item. No
// filesystem or frontend fallback is permitted when protected storage fails.
type Store interface {
	Load() ([]byte, error)
	Save([]byte) error
	Delete() error
}

type Config struct {
	AuthorizeEndpoint string
	TokenEndpoint     string
	DeviceName        string
	Platform          string
	Network           Network
	Store             Store
	OpenBrowser       func(string) error
	// Ready receives only a native token provider after protected persistence.
	// It may install a real event consumer; no discard consumer is supplied.
	Ready        func(context.Context, *Manager) error
	SignedOut    func() error
	ProofTargets []ProofTarget
}

type ProofTarget struct{ Method, URL string }

func authority(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery && !strings.HasSuffix(u.Host, ":")
}

func validConfig(c Config) bool {
	return authority(c.AuthorizeEndpoint) && authority(c.TokenEndpoint) && c.Network != nil && c.Store != nil && c.OpenBrowser != nil && validPlatform(c.Platform) && len(c.ProofTargets) <= 8
}

func validPlatform(value string) bool {
	return value == "macos" || value == "windows" || value == "linux"
}

func verifiedTLS(source *tls.Config, endpoint string) (*tls.Config, error) {
	result := &tls.Config{MinVersion: tls.VersionTLS12}
	if source != nil {
		result = source.Clone()
	}
	u, _ := url.Parse(endpoint)
	if result.InsecureSkipVerify || result.ServerName != "" && result.ServerName != u.Hostname() {
		return nil, ErrConfiguration
	}
	if result.MinVersion < tls.VersionTLS12 {
		result.MinVersion = tls.VersionTLS12
	}
	return result, nil
}
