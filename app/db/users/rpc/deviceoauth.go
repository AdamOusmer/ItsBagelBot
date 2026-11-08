// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"github.com/nats-io/nats.go"
)

// SubscribeDeviceOAuth is separate from dashboard consent/account-management
// subjects. Only the native Elixir auth boundary receives these permissions.
func SubscribeDeviceOAuth(w Wiring, prefix string) error {
	d := &deviceAuthRPC{repo: w.Repo}
	return serveMsgVerbs(w.Within(deviceAuthTimeout), prefix, map[string]func(context.Context, *nats.Msg){"token": d.handleOAuthToken, "authorize": d.handleDeviceAuthorize, "revoke": d.handleDeviceSelfRevoke})
}
func (d *deviceAuthRPC) handleOAuthToken(ctx context.Context, msg *nats.Msg) {
	request, ok := decodeRequest[usersrpc.DeviceOAuthRequest](msg)
	if !ok {
		return
	}
	result, err := d.repo.DeviceOAuth(ctx, repository.DeviceOAuthRequest{GrantType: request.GrantType, ClientID: request.ClientID, Code: request.Code, CodeVerifier: request.CodeVerifier, RedirectURI: request.RedirectURI, RefreshToken: request.RefreshToken, Proof: request.Proof, URL: request.URL})
	if err != nil {
		bus.Respond(msg, usersrpc.DeviceOAuthReply{DeviceTokenReply: deviceTokenFailure(err)})
		return
	}
	reply := deviceTokenReply(result.Tokens)
	reply.OAuthError = result.OAuthError
	bus.Respond(msg, usersrpc.DeviceOAuthReply{DeviceTokenReply: reply, Nonce: result.Nonce})
}
func (d *deviceAuthRPC) handleDeviceAuthorize(ctx context.Context, msg *nats.Msg) {
	request, ok := decodeRequest[usersrpc.DeviceAuthorizeRequest](msg)
	if !ok {
		return
	}
	claims, nonce, err := d.repo.AuthorizeDevice(ctx, request.Token, request.Proof, request.Method, request.URL)
	if err != nil {
		bus.Respond(msg, authorizeDeviceFailure(err, nonce))
		return
	}
	bus.Respond(msg, usersrpc.DeviceAuthorizeReply{UserID: claims.Sub, DeviceID: claims.DeviceID, GrantID: claims.GrantID, ExpiresAt: claims.Exp})
}
func authorizeDeviceFailure(err error, nonce string) usersrpc.DeviceAuthorizeReply {
	switch {
	case errors.Is(err, repository.ErrDeviceNonce):
		return usersrpc.DeviceAuthorizeReply{OAuthError: "use_dpop_nonce", Nonce: nonce}
	case errors.Is(err, repository.ErrDeviceProof):
		return usersrpc.DeviceAuthorizeReply{OAuthError: "invalid_dpop_proof"}
	case errors.Is(err, repository.ErrDeviceInvalidGrant):
		return usersrpc.DeviceAuthorizeReply{OAuthError: "invalid_token"}
	default:
		return usersrpc.DeviceAuthorizeReply{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, "device authorization unavailable")}
	}
}

func (d *deviceAuthRPC) handleDeviceSelfRevoke(ctx context.Context, msg *nats.Msg) {
	request, ok := decodeRequest[usersrpc.DeviceAuthorizeRequest](msg)
	if !ok {
		return
	}
	claims, nonce, err := d.repo.AuthorizeDevice(ctx, request.Token, request.Proof, request.Method, request.URL)
	if err != nil {
		bus.Respond(msg, authorizeDeviceFailure(err, nonce))
		return
	}
	userID, err := strconv.ParseUint(claims.Sub, 10, 64)
	if err != nil {
		bus.Respond(msg, authorizeDeviceFailure(repository.ErrDeviceInvalidGrant, ""))
		return
	}
	_, err = d.repo.RevokeDeviceGrant(ctx, userID, claims.GrantID)
	if err != nil {
		bus.Respond(msg, authorizeDeviceFailure(err, ""))
		return
	}
	bus.Respond(msg, map[string]bool{"ok": true})
}
