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

// QuotesWiring bundles what SubscribeQuotes needs, mirroring the govee wiring
// so the subscribe entry point stays a single argument instead of a long
// parameter list.
type QuotesWiring struct {
	NC         *nats.Conn
	Repo       *repository.Quotes
	Prefix     string // subject prefix, e.g. "bagel.rpc.modules.quote"
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

// SubscribeQuotes answers the channel-quotes verbs under w.Prefix (default
// "bagel.rpc.modules.quote"): add, get, random, remove. They ride the same
// MODULES_RPC account export as the dashboard verbs, so no ACL change is
// needed for sesame to call them.
func SubscribeQuotes(w QuotesWiring) error {
	q := &quotesRPC{repo: w.Repo, log: w.Log}

	verbs := []struct {
		verb    string
		handler func(context.Context, modulesrpc.QuoteRequest) modulesrpc.QuoteReply
	}{
		{"add", q.handleAdd},
		{"get", q.handleGet},
		{"random", q.handleRandom},
		{"remove", q.handleRemove},
		{"list", q.handleList},
	}

	for _, v := range verbs {
		subject := w.Prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[modulesrpc.QuoteRequest, modulesrpc.QuoteReply](w.NC, subject, w.QueueGroup, 2*time.Second, w.App, w.Log, v.handler); err != nil {
			return err
		}
	}
	return nil
}

// withUserID parses the broadcaster id and runs fn, or returns an
// "invalid user_id" reply. It removes the parse-and-guard prologue every verb
// would otherwise repeat.
func (q *quotesRPC) withUserID(req modulesrpc.QuoteRequest, fn func(uint64) modulesrpc.QuoteReply) modulesrpc.QuoteReply {
	id, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return modulesrpc.QuoteReply{Error: "invalid user_id"}
	}
	return fn(id)
}

// errReply maps a repository error onto the reply's error envelope, or falls
// back to ok when there was none.
func errReply(err error, ok modulesrpc.QuoteReply) modulesrpc.QuoteReply {
	if err != nil {
		return modulesrpc.QuoteReply{Error: err.Error()}
	}
	return ok
}

func (q *quotesRPC) handleAdd(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	return q.withUserID(req, func(id uint64) modulesrpc.QuoteReply {
		draft := repository.QuoteDraft{Text: req.Text, AddedBy: req.AddedBy}
		if req.CreatedAt != "" {
			createdAt, err := time.Parse(time.RFC3339, req.CreatedAt)
			if err != nil {
				return modulesrpc.QuoteReply{Error: "invalid quote date"}
			}
			draft.CreatedAt = createdAt
		}
		view, err := q.repo.Add(ctx, id, draft)
		return errReply(err, modulesrpc.QuoteReply{Quote: view, Found: true})
	})
}

func (q *quotesRPC) handleGet(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	return q.withUserID(req, func(id uint64) modulesrpc.QuoteReply {
		view, found, err := q.repo.Get(ctx, id, req.Number)
		return errReply(err, modulesrpc.QuoteReply{Quote: view, Found: found})
	})
}

func (q *quotesRPC) handleRandom(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	return q.withUserID(req, func(id uint64) modulesrpc.QuoteReply {
		view, found, err := q.repo.Random(ctx, id)
		return errReply(err, modulesrpc.QuoteReply{Quote: view, Found: found})
	})
}

func (q *quotesRPC) handleRemove(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	return q.withUserID(req, func(id uint64) modulesrpc.QuoteReply {
		found, err := q.repo.Remove(ctx, id, req.Number)
		return errReply(err, modulesrpc.QuoteReply{Found: found})
	})
}

func (q *quotesRPC) handleList(ctx context.Context, req modulesrpc.QuoteRequest) modulesrpc.QuoteReply {
	return q.withUserID(req, func(id uint64) modulesrpc.QuoteReply {
		quotes, err := q.repo.List(ctx, id)
		return errReply(err, modulesrpc.QuoteReply{Quotes: quotes})
	})
}
