package rpc

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Chatters listing pages through Helix (1000 chatters per page), so a big
// channel takes a few sequential round trips; the budget covers ~30 pages
// while staying under the caller's tick interval by orders of magnitude.
const chattersHandleTimeout = 10 * time.Second

type chatters struct {
	twitch *twitch.Client
	botID  string
	log    *zap.Logger
}

// SubscribeChatters registers the chatter listing verb under prefix:
//
//	<prefix>.chatters.get  {broadcaster_id} -> {chatters}
//
// It backs sesame's loyalty watch tick: one call per live channel per tick,
// under the bot's own user token (moderator:read:chatters). botID is the bot
// account's Twitch user id, required as moderator_id on the Helix call.
func SubscribeChatters(nc *nats.Conn, tw *twitch.Client, botID, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	c := &chatters{twitch: tw, botID: botID, log: log}
	subject := prefix + ".chatters.get"
	return bus.QueueSubscribeJSON[manage.ChattersRequest, manage.ChattersReply](nc, subject, queueGroup, chattersHandleTimeout, app, log, c.handleGet)
}

func (c *chatters) handleGet(ctx context.Context, req manage.ChattersRequest) manage.ChattersReply {
	if req.BroadcasterID == "" || c.botID == "" {
		return manage.ChattersReply{Error: "bad request"}
	}
	list, err := c.twitch.GetChatters(ctx, req.BroadcasterID, c.botID)
	if err != nil {
		if errors.Is(err, twitch.ErrMissingScope) || errors.Is(err, twitch.ErrNoUserToken) {
			return manage.ChattersReply{MissingScope: true, Error: "chatters unavailable"}
		}
		c.log.Warn("chatters get failed", zap.String("broadcaster_id", req.BroadcasterID), zap.Error(err))
		return manage.ChattersReply{Error: "twitch request failed"}
	}
	out := make([]manage.Chatter, 0, len(list))
	for _, ch := range list {
		out = append(out, manage.Chatter{ID: ch.ID, Login: ch.Login})
	}
	return manage.ChattersReply{Chatters: out}
}
