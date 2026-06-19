package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/pkg/bus"

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

func SubscribeTokens(nc *nats.Conn, repo *repository.Users, prefix, queueGroup string, log *zap.Logger) error {
	t := &tokensRPC{repo: repo, log: log}

	verbs := map[string]func(context.Context, tokensRequest) tokensReply{
		"get":  t.handleGet,
		"save": t.handleSave,
	}
	for verb, handle := range verbs {
		subject := prefix + "." + verb
		if err := bus.QueueSubscribeJSON[tokensRequest, tokensReply](nc, subject, queueGroup, 3*time.Second, log, handle); err != nil {
			return err
		}
	}
	return nil
}

type tokensRequest struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type tokensReply struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (t *tokensRPC) handleGet(ctx context.Context, req tokensRequest) tokensReply {
	id, err := parseTokensUser(req)
	if err != nil {
		return tokensReply{Error: err.Error()}
	}

	access, refresh, err := t.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		return tokensReply{Error: err.Error()}
	}

	return tokensReply{
		AccessToken:  string(access),
		RefreshToken: string(refresh),
	}
}

func (t *tokensRPC) handleSave(ctx context.Context, req tokensRequest) tokensReply {
	id, err := parseTokensUser(req)
	if err != nil {
		return tokensReply{Error: err.Error()}
	}

	if err := t.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		t.log.Error("tokens save", zap.Error(err))
		return tokensReply{Error: err.Error()}
	}

	return tokensReply{}
}

func parseTokensUser(req tokensRequest) (uint64, error) {
	if req.UserID == "" {
		return 0, fmt.Errorf("bad request")
	}
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("user_id must be numeric")
	}
	return id, nil
}
