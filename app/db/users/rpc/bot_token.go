// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	twitchValidateURL      = "https://id.twitch.tv/oauth2/validate"
	twitchValidateTimeout  = 2 * time.Second
	twitchValidateMaxBytes = 64 << 10
)

var botRequiredScopes = []string{"user:bot", "user:read:chat", "user:write:chat"}

var (
	errBotIdentityRequired = errors.New("configured bot identity required")
	errBotTokensRequired   = errors.New("bot OAuth tokens required")
	errBotGrantRejected    = errors.New("bot OAuth grant rejected")
	errBotGrantUnverified  = errors.New("bot OAuth grant could not be verified")
)

var botGrantRules = []domainrpc.Rule{
	domainrpc.Is(errBotIdentityRequired, domainrpc.CodeForbidden),
	domainrpc.Is(errBotTokensRequired, domainrpc.CodeInvalid),
	domainrpc.Is(errBotGrantRejected, domainrpc.CodeForbidden),
	domainrpc.Is(errBotGrantUnverified, domainrpc.CodeUnavailable),
}

type twitchGrant struct {
	UserID string   `json:"user_id"`
	Scopes []string `json:"scopes"`
}

type grantValidator func(ctx context.Context, accessToken string) (twitchGrant, error)

func twitchGrantValidator(client *http.Client, endpoint string) grantValidator {
	return func(ctx context.Context, accessToken string) (twitchGrant, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return twitchGrant{}, fmt.Errorf("%w: %w", errBotGrantUnverified, err)
		}
		req.Header.Set("Authorization", "OAuth "+accessToken)
		res, err := client.Do(req)
		if err != nil {
			return twitchGrant{}, fmt.Errorf("%w: %w", errBotGrantUnverified, err)
		}
		defer res.Body.Close()
		return decodeTwitchGrant(res)
	}
}

func decodeTwitchGrant(res *http.Response) (twitchGrant, error) {
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return twitchGrant{}, fmt.Errorf("%w: Twitch reports the access token invalid", errBotGrantRejected)
	default:
		return twitchGrant{}, fmt.Errorf("%w: Twitch validate status %d", errBotGrantUnverified, res.StatusCode)
	}
	var grant twitchGrant
	if err := codec.NewDecoder(io.LimitReader(res.Body, twitchValidateMaxBytes)).Decode(&grant); err != nil {
		return twitchGrant{}, fmt.Errorf("%w: %w", errBotGrantUnverified, err)
	}
	return grant, nil
}

// bot_token_set skips the staff gate, so the grant itself must prove it belongs to the configured bot.
func (a *adminRPC) botTokenSet(ctx context.Context, req usersrpc.AdminRequest) usersrpc.AdminReply {
	if err := a.checkBotGrant(ctx, req); err != nil {
		monitor.TxnLogger(ctx, a.log).Warn("bot token refused", zap.Error(err))
		return adminError(domainrpc.Fail(err, botGrantRules...))
	}
	return a.tokenSet(ctx, req)
}

func (a *adminRPC) checkBotGrant(ctx context.Context, req usersrpc.AdminRequest) error {
	if a.botUserID == "" {
		return errBotIdentityRequired
	}
	if req.UserID != a.botUserID || req.ActorID != a.botUserID {
		return errBotIdentityRequired
	}
	if req.AccessToken == "" || req.RefreshToken == "" {
		return errBotTokensRequired
	}
	grant, err := a.validateBotGrant(ctx, req.AccessToken)
	if err != nil {
		return err
	}
	return a.checkGrantOwner(grant)
}

func (a *adminRPC) checkGrantOwner(grant twitchGrant) error {
	if grant.UserID != a.botUserID {
		return fmt.Errorf("%w: access token belongs to Twitch user %q", errBotGrantRejected, grant.UserID)
	}
	var missing []string
	for _, scope := range botRequiredScopes {
		if !slices.Contains(grant.Scopes, scope) {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing scopes %s", errBotGrantRejected, strings.Join(missing, " "))
	}
	return nil
}

func logBotIdentity(log *zap.Logger, botUserID string) {
	if botUserID == "" {
		log.Warn("TWITCH_BOT_USER_ID is unset; bot_token_set refuses every bot authorization")
		return
	}
	log.Info("bot identity configured", zap.String("bot_user_id", botUserID))
}
