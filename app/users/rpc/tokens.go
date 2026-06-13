package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/repository"

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

	verbs := map[string]func(*nats.Msg){
		"get":  t.handleGet,
		"save": t.handleSave,
	}
	for verb, handle := range verbs {
		subject := prefix + "." + verb
		if _, err := nc.QueueSubscribe(subject, queueGroup, handle); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
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

func (t *tokensRPC) handleGet(msg *nats.Msg) {
	id, ok := parseTokensUser(msg)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	access, refresh, err := t.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	if err != nil {
		respondTokens(msg, tokensReply{Error: err.Error()})
		return
	}

	respondTokens(msg, tokensReply{
		AccessToken:  string(access),
		RefreshToken: string(refresh),
	})
}

func (t *tokensRPC) handleSave(msg *nats.Msg) {
	id, ok := parseTokensUser(msg)
	if !ok {
		return
	}

	var req tokensRequest
	_ = json.Unmarshal(msg.Data, &req)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := t.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch,
		[]byte(req.AccessToken), []byte(req.RefreshToken)); err != nil {
		t.log.Error("tokens save", zap.Error(err))
		respondTokens(msg, tokensReply{Error: err.Error()})
		return
	}

	respondTokens(msg, tokensReply{})
}

func parseTokensUser(msg *nats.Msg) (uint64, bool) {
	var req tokensRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil || req.UserID == "" {
		respondTokens(msg, tokensReply{Error: "bad request"})
		return 0, false
	}
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		respondTokens(msg, tokensReply{Error: "user_id must be numeric"})
		return 0, false
	}
	return id, true
}

func respondTokens(msg *nats.Msg, reply tokensReply) {
	body, _ := json.Marshal(reply)
	_ = msg.Respond(body)
}
