package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"ItsBagelBot/app/users/ent/tokens"
)

// tokensRPC serves the internal token verbs other services use to operate as
// the bot account: outgress loads the bot's refresh token at renewal time and
// writes the rotated one back, so a restart never resurrects a stale token.
// Plaintext only ever transits these subjects; NATS authorization restricts
// who may subscribe to them.
type tokensRPC struct {
	repo *repository.Users
	log  *zap.Logger
}

func SubscribeTokens(w Wiring, prefix string) error {
	nc, repo, app, log, queueGroup := w.NC, w.Repo, w.App, w.Log, w.Queue
	t := &tokensRPC{repo: repo, log: log}

	verbs := map[string]func(context.Context, usersrpc.TokensRequest) usersrpc.TokensReply{
		"get":  t.handleGet,
		"save": t.handleSave,
	}
	for verb, handle := range verbs {
		subject := prefix + "." + verb
		if err := bus.QueueSubscribeJSON[usersrpc.TokensRequest, usersrpc.TokensReply](nc, subject, queueGroup, 2*time.Second, app, log, handle); err != nil {
			return err
		}
	}
	return nil
}

func (t *tokensRPC) handleGet(ctx context.Context, req usersrpc.TokensRequest) usersrpc.TokensReply {
	id, err := parseTokensUser(req)
	if err != nil {
		return usersrpc.TokensReply{Error: err.Error()}
	}

	access, refresh, err := t.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		return usersrpc.TokensReply{Error: err.Error()}
	}

	return usersrpc.TokensReply{
		AccessToken:  string(access),
		RefreshToken: string(refresh),
	}
}

func (t *tokensRPC) handleSave(ctx context.Context, req usersrpc.TokensRequest) usersrpc.TokensReply {
	log := monitor.TxnLogger(ctx, t.log)
	id, err := parseTokensUser(req)
	if err != nil {
		return usersrpc.TokensReply{Error: err.Error()}
	}

	if err := t.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		log.Error("tokens save", zap.Error(err))
		return usersrpc.TokensReply{Error: err.Error()}
	}

	return usersrpc.TokensReply{}
}

func parseTokensUser(req usersrpc.TokensRequest) (uint64, error) {
	if req.UserID == "" {
		return 0, fmt.Errorf("bad request")
	}
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("user_id must be numeric")
	}
	return id, nil
}
