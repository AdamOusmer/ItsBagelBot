package rpc

import (
	"context"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
)

type quotesRPC struct {
	repo *repository.Quotes
	log  *zap.Logger
}

// SubscribeQuotes answers the channel-quotes verbs under prefix (default
// "bagel.rpc.modules.quote"): add, get, random, remove. They ride the same
// MODULES_RPC account export as the dashboard verbs, so no ACL change is
// needed for sesame to call them.
func SubscribeQuotes(nc *nats.Conn, repo *repository.Quotes, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	q := &quotesRPC{repo: repo, log: log}

	verbs := []struct {
		verb    string
		handler func(context.Context, modulesrpc.QuoteRequest) modulesrpc.QuoteReply
	}{
		{"add", q.handleAdd},
		{"get", q.handleGet},
		{"random", q.handleRandom},
		{"remove", q.handleRemove},
	}

	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[modulesrpc.QuoteRequest, modulesrpc.QuoteReply](nc, subject, queueGroup, 2*time.Second, app, log, v.handler); err != nil {
			return err
		}
	}
	return nil
}

func (q *quotesRPC) parseUserID(req modulesrpc.QuoteRequest) (uint64, bool, modulesrpc.QuoteReply) {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, false, modulesrpc.QuoteReply{Error: "invalid user_id"}
	}
	return id, true, modulesrpc.QuoteReply{}
}

func (q *quotesRPC) handleAdd(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	id, ok, reply := q.parseUserID(req)
	if !ok {
		return reply
	}
	view, err := q.repo.Add(ctx, id, req.Text, req.AddedBy)
	if err != nil {
		return modulesrpc.QuoteReply{Error: err.Error()}
	}
	return modulesrpc.QuoteReply{Quote: view, Found: true}
}

func (q *quotesRPC) handleGet(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	id, ok, reply := q.parseUserID(req)
	if !ok {
		return reply
	}
	view, found, err := q.repo.Get(ctx, id, req.Number)
	if err != nil {
		return modulesrpc.QuoteReply{Error: err.Error()}
	}
	return modulesrpc.QuoteReply{Quote: view, Found: found}
}

func (q *quotesRPC) handleRandom(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	id, ok, reply := q.parseUserID(req)
	if !ok {
		return reply
	}
	view, found, err := q.repo.Random(ctx, id)
	if err != nil {
		return modulesrpc.QuoteReply{Error: err.Error()}
	}
	return modulesrpc.QuoteReply{Quote: view, Found: found}
}

func (q *quotesRPC) handleRemove(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	id, ok, reply := q.parseUserID(req)
	if !ok {
		return reply
	}
	found, err := q.repo.Remove(ctx, id, req.Number)
	if err != nil {
		return modulesrpc.QuoteReply{Error: err.Error()}
	}
	return modulesrpc.QuoteReply{Found: found}
}
