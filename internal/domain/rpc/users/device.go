// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package usersrpc

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

// OAuth 2.0 token-endpoint error codes (RFC 6749 5.2) that Users may return
// for exchange/refresh; Dashboard maps these straight into the JSON body.
const (
	OAuthErrorInvalidGrant   = "invalid_grant"
	OAuthErrorInvalidRequest = "invalid_request"
	OAuthErrorInvalidClient  = "invalid_client"
)

type DeviceCreateCodeRequest struct {
	UserID        string `json:"user_id"`
	CodeChallenge string `json:"code_challenge"`
	RedirectURI   string `json:"redirect_uri"`
	DeviceName    string `json:"device_name"`
	Platform      string `json:"platform"`
	DPoPJKT       string `json:"dpop_jkt"`
}

type DeviceCreateCodeReply struct {
	Code string `json:"code,omitempty"`
	rpc.Refusal
}

type DeviceExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

type DeviceRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// DeviceTokenReply answers both exchange and refresh.
type DeviceTokenReply struct {
	AccessToken  string `json:"access_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	GrantID      string `json:"grant_id,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
	OAuthError   string `json:"oauth_error,omitempty"`
	rpc.Refusal
}

// DeviceOAuthRequest is sent only by the independent native auth HTTP boundary.
// URL is its configured public URL, never a client-controlled forwarding header.
type DeviceOAuthRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	Code         string `json:"code,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Proof        string `json:"proof"`
	URL          string `json:"url"`
}

type DeviceOAuthReply struct {
	DeviceTokenReply
	Nonce string `json:"dpop_nonce,omitempty"`
}

type DeviceAuthorizeRequest struct {
	Token  string `json:"token"`
	Proof  string `json:"proof"`
	Method string `json:"method"`
	URL    string `json:"url"`
}

type DeviceAuthorizeReply struct {
	UserID     string `json:"user_id,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	GrantID    string `json:"grant_id,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	OAuthError string `json:"oauth_error,omitempty"`
	Nonce      string `json:"dpop_nonce,omitempty"`
	rpc.Refusal
}

type DeviceListRequest struct {
	UserID string `json:"user_id"`
}

type DeviceGrantView struct {
	GrantID    string     `json:"grant_id"`
	DeviceID   string     `json:"device_id"`
	DeviceName string     `json:"device_name"`
	Platform   string     `json:"platform"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
}

type DeviceListReply struct {
	Grants []DeviceGrantView `json:"grants,omitempty"`
	rpc.Refusal
}

type DeviceRevokeRequest struct {
	UserID  string `json:"user_id"`
	GrantID string `json:"grant_id"`
}

type DeviceRevokeReply struct {
	DeviceID string `json:"device_id,omitempty"`
	rpc.Refusal
}

type DeviceRevokeAllRequest struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason,omitempty"`
}

type DeviceRevokeAllReply struct {
	DeviceIDs []string `json:"device_ids,omitempty"`
	rpc.Refusal
}
