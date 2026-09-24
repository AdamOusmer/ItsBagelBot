// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"ItsBagelBot/app/db/users/ent/tokens"
)

type tokensRPC struct {
	repo *repository.Users
	log  *zap.Logger
}

func SubscribeTokens(w Wiring, prefix string) error {
	t := &tokensRPC{repo: w.Repo, log: w.Log}

	return bus.ServeVerbs(w.Within(tokensBudget), prefix,
		bus.VerbForUser[usersrpc.TokensRequest, usersrpc.TokensReply]("get", t.handleGet),
		bus.VerbForUser[usersrpc.TokensRequest, usersrpc.TokensReply]("save", t.handleSave),
	)
}

func (t *tokensRPC) handleGet(ctx context.Context, _ usersrpc.TokensRequest, id uint64) (usersrpc.TokensReply, error) {
	access, refresh, expiresAt, err := t.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		return usersrpc.TokensReply{}, err
	}

	return usersrpc.TokensReply{
		AccessToken:          string(access),
		RefreshToken:         string(refresh),
		AccessTokenExpiresAt: expiresAt,
	}, nil
}

func (t *tokensRPC) handleSave(ctx context.Context, req usersrpc.TokensRequest, id uint64) (usersrpc.TokensReply, error) {
	err := t.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken), req.AccessTokenExpiresAt)
	if err != nil {
		monitor.TxnLogger(ctx, t.log).Error("tokens save", zap.Error(err))
	}
	return usersrpc.TokensReply{}, err
}
