// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"

	"ItsBagelBot/internal/domain/validate"
)

// DeviceOAuthRequest comes from the scoped Elixir HTTP adapter. The client
// never chooses the configured public proof URL through forwarding headers.
type DeviceOAuthRequest struct {
	GrantType, ClientID, Code, CodeVerifier, RedirectURI, RefreshToken, Proof, URL string
}
type DeviceOAuthResult struct {
	Tokens            DeviceTokens
	OAuthError, Nonce string
}

func (r *Users) DeviceOAuth(ctx context.Context, request DeviceOAuthRequest) (DeviceOAuthResult, error) {
	if r.deviceAuth.TokenURL == "" || request.URL != r.deviceAuth.TokenURL {
		return DeviceOAuthResult{}, ErrDeviceSigningKeyUnavailable
	}
	if request.ClientID != "itsbagelbot-desktop" {
		return DeviceOAuthResult{OAuthError: "invalid_client"}, nil
	}
	if request.GrantType != "authorization_code" && request.GrantType != "refresh_token" {
		return DeviceOAuthResult{OAuthError: "unsupported_grant_type"}, nil
	}
	if r.deviceSigner == nil {
		return DeviceOAuthResult{}, ErrDeviceSigningKeyUnavailable
	}
	jkt, nonce, err := r.verifyDeviceProof(ctx, request.Proof, "POST", request.URL, "")
	if err != nil {
		return deviceOAuthFailure(err, nonce)
	}
	tokens, err := r.deviceOAuthGrant(ctx, request, jkt)
	if err != nil {
		return deviceOAuthFailure(err, "")
	}
	return DeviceOAuthResult{Tokens: tokens}, nil
}
func (r *Users) deviceOAuthGrant(ctx context.Context, request DeviceOAuthRequest, jkt string) (DeviceTokens, error) {
	if request.GrantType == "refresh_token" {
		return r.refreshDeviceGrant(ctx, request.RefreshToken, jkt)
	}
	if validate.DeviceVerifier(request.CodeVerifier) != nil || validate.DeviceRedirectURI(request.RedirectURI) != nil || request.Code == "" || len(request.Code) > 512 {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}
	return r.exchangeDeviceCode(ctx, request.Code, request.CodeVerifier, request.RedirectURI, jkt)
}
func deviceOAuthFailure(err error, nonce string) (DeviceOAuthResult, error) {
	switch {
	case errors.Is(err, ErrDeviceNonce):
		return DeviceOAuthResult{OAuthError: "use_dpop_nonce", Nonce: nonce}, nil
	case errors.Is(err, ErrDeviceProof):
		return DeviceOAuthResult{OAuthError: "invalid_dpop_proof"}, nil
	case errors.Is(err, ErrDeviceInvalidGrant):
		return DeviceOAuthResult{OAuthError: "invalid_grant"}, nil
	default:
		return DeviceOAuthResult{}, err
	}
}
