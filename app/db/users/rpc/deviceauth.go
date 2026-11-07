// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
)

const deviceAuthTimeout = 3 * time.Second

type deviceAuthRPC struct {
	repo *repository.Users
}

// SubscribeDeviceAuth wires desktop device authorization. Only exchange and
// refresh require the Ed25519 signing key; they fail closed on their own
// when it is unconfigured, so the rest of this surface keeps working.
func SubscribeDeviceAuth(w Wiring, prefix string) error {
	d := &deviceAuthRPC{repo: w.Repo}
	verbs := map[string]func(context.Context, *nats.Msg){
		"create_code": d.handleCreateCode,
		"exchange":    d.handleExchange,
		"refresh":     d.handleRefresh,
		"list":        d.handleList,
		"revoke":      d.handleRevoke,
		"revoke_all":  d.handleRevokeAll,
	}
	return serveMsgVerbs(w.Within(deviceAuthTimeout), prefix, verbs)
}

func (d *deviceAuthRPC) handleCreateCode(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.DeviceCreateCodeRequest](msg)
	if !ok {
		return
	}
	userID, ok := parseWireID(msg, req.UserID, "user_id")
	if !ok {
		return
	}
	if err := validateDeviceCreateInput(req); err != nil {
		respondErr(msg, err.Error())
		return
	}

	code, err := d.repo.CreateDeviceAuthorizationCode(ctx, userID, repository.DeviceCodeRequest{
		Challenge: req.CodeChallenge, RedirectURI: req.RedirectURI,
		DeviceName: validate.DeviceName(req.DeviceName), Platform: req.Platform,
	})
	if err != nil {
		respondFail(msg, err)
		return
	}
	bus.Respond(msg, usersrpc.DeviceCreateCodeReply{Code: code})
}

func validateDeviceCreateInput(req usersrpc.DeviceCreateCodeRequest) error {
	if err := validate.DeviceChallenge(req.CodeChallenge); err != nil {
		return err
	}
	if err := validate.DeviceRedirectURI(req.RedirectURI); err != nil {
		return err
	}
	return validate.DevicePlatform(req.Platform)
}

func (d *deviceAuthRPC) handleExchange(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.DeviceExchangeRequest](msg)
	if !ok {
		return
	}
	if err := validateDeviceExchangeInput(req); err != nil {
		bus.Respond(msg, deviceTokenInputError(err))
		return
	}

	tokens, err := d.repo.ExchangeDeviceCode(ctx, req.Code, req.CodeVerifier, req.RedirectURI)
	if err != nil {
		bus.Respond(msg, deviceTokenFailure(err))
		return
	}
	bus.Respond(msg, deviceTokenReply(tokens))
}

func validateDeviceExchangeInput(req usersrpc.DeviceExchangeRequest) error {
	if err := validate.DeviceVerifier(req.CodeVerifier); err != nil {
		return err
	}
	return validate.DeviceRedirectURI(req.RedirectURI)
}

func (d *deviceAuthRPC) handleRefresh(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.DeviceRefreshRequest](msg)
	if !ok {
		return
	}
	tokens, err := d.repo.RefreshDeviceGrant(ctx, req.RefreshToken)
	if err != nil {
		bus.Respond(msg, deviceTokenFailure(err))
		return
	}
	bus.Respond(msg, deviceTokenReply(tokens))
}

func deviceTokenReply(t repository.DeviceTokens) usersrpc.DeviceTokenReply {
	return usersrpc.DeviceTokenReply{
		AccessToken: t.AccessToken, TokenType: "Bearer", ExpiresIn: t.ExpiresIn,
		RefreshToken: t.RefreshToken, GrantID: t.GrantID, DeviceID: t.DeviceID,
	}
}

func deviceTokenInputError(err error) usersrpc.DeviceTokenReply {
	return usersrpc.DeviceTokenReply{
		OAuthError: usersrpc.OAuthErrorInvalidRequest,
		Refusal:    domainrpc.Refused(domainrpc.CodeInvalid, err.Error()),
	}
}

func deviceTokenFailure(err error) usersrpc.DeviceTokenReply {
	if errors.Is(err, repository.ErrDeviceInvalidGrant) {
		return usersrpc.DeviceTokenReply{
			OAuthError: usersrpc.OAuthErrorInvalidGrant,
			Refusal:    domainrpc.Refused(domainrpc.CodeInvalid, err.Error()),
		}
	}
	if errors.Is(err, repository.ErrDeviceSigningKeyUnavailable) {
		return usersrpc.DeviceTokenReply{Refusal: domainrpc.Refused(domainrpc.CodeUnavailable, err.Error())}
	}
	return usersrpc.DeviceTokenReply{Refusal: domainrpc.Fail(err)}
}

func (d *deviceAuthRPC) handleList(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.DeviceListRequest](msg)
	if !ok {
		return
	}
	userID, ok := parseWireID(msg, req.UserID, "user_id")
	if !ok {
		return
	}

	views, err := d.repo.ListDeviceGrants(ctx, userID)
	if err != nil {
		respondFail(msg, err)
		return
	}
	bus.Respond(msg, usersrpc.DeviceListReply{Grants: deviceGrantViews(views)})
}

func deviceGrantViews(views []repository.DeviceGrantView) []usersrpc.DeviceGrantView {
	grants := make([]usersrpc.DeviceGrantView, 0, len(views))
	for _, v := range views {
		grants = append(grants, usersrpc.DeviceGrantView{
			GrantID: v.GrantID, DeviceID: v.DeviceID, DeviceName: v.DeviceName, Platform: v.Platform,
			CreatedAt: v.CreatedAt, LastUsedAt: v.LastUsedAt, ExpiresAt: v.ExpiresAt,
		})
	}
	return grants
}

func (d *deviceAuthRPC) handleRevoke(ctx context.Context, msg *nats.Msg) {
	serveDeviceRevoke(msg, func(req usersrpc.DeviceRevokeRequest) string { return req.UserID },
		func(userID uint64, req usersrpc.DeviceRevokeRequest) (usersrpc.DeviceRevokeReply, error) {
			deviceID, err := d.repo.RevokeDeviceGrant(ctx, userID, req.GrantID)
			return usersrpc.DeviceRevokeReply{DeviceID: deviceID}, err
		})
}

func (d *deviceAuthRPC) handleRevokeAll(ctx context.Context, msg *nats.Msg) {
	serveDeviceRevoke(msg, func(req usersrpc.DeviceRevokeAllRequest) string { return req.UserID },
		func(userID uint64, req usersrpc.DeviceRevokeAllRequest) (usersrpc.DeviceRevokeAllReply, error) {
			deviceIDs, err := d.repo.RevokeAllDeviceGrants(ctx, userID, deviceRevokeReason(req.Reason))
			return usersrpc.DeviceRevokeAllReply{DeviceIDs: deviceIDs}, err
		})
}

func serveDeviceRevoke[Req, Reply any](msg *nats.Msg, wireUser func(Req) string, revoke func(uint64, Req) (Reply, error)) {
	req, ok := decodeRequest[Req](msg)
	if !ok {
		return
	}
	userID, ok := parseWireID(msg, wireUser(req), "user_id")
	if !ok {
		return
	}
	reply, err := revoke(userID, req)
	if err != nil {
		respondFail(msg, err, domainrpc.Is(repository.ErrDeviceGrantNotFound, domainrpc.CodeNotFound))
		return
	}
	bus.Respond(msg, reply)
}

func deviceRevokeReason(reason string) string {
	if reason == "" {
		return "user"
	}
	return reason
}
