// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
)

type quotesRPC struct {
	repo *repository.Quotes
	log  *zap.Logger
}

func SubscribeQuotes(w bus.RPCWiring, repo *repository.Quotes, prefix string) error {
	q := &quotesRPC{repo: repo, log: w.Log}

	return bus.ServeVerbs(w, prefix,
		quoteVerb("add", q.handleAdd),
		quoteVerb("get", q.handleGet),
		quoteVerb("random", q.handleRandom),
		quoteVerb("search", q.handleSearch),
		quoteVerb("edit", q.handleEdit),
		quoteVerb("remove", q.handleRemove),
		quoteVerb("list", q.handleList),
	)
}

func quoteVerb(name string, load func(context.Context, modulesrpc.QuoteRequest, uint64) (modulesrpc.QuoteReply, error)) bus.Verb[modulesrpc.QuoteRequest, modulesrpc.QuoteReply] {
	return bus.VerbForUser[modulesrpc.QuoteRequest, modulesrpc.QuoteReply](name, load)
}

func parseQuoteDate(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}

func (q *quotesRPC) handleAdd(ctx context.Context, req modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	createdAt, ok := parseQuoteDate(req.CreatedAt)
	if !ok {
		return modulesrpc.QuoteReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid quote date")}, nil
	}
	draft := repository.QuoteDraft{Text: req.Text, AddedBy: req.AddedBy, CreatedAt: createdAt}
	view, err := q.repo.Add(ctx, id, draft)
	return modulesrpc.QuoteReply{Quote: view, Found: true}, err
}

func (q *quotesRPC) handleGet(ctx context.Context, req modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	view, found, err := q.repo.Get(ctx, id, req.Number)
	return modulesrpc.QuoteReply{Quote: view, Found: found}, err
}

func (q *quotesRPC) handleRandom(ctx context.Context, _ modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	view, found, err := q.repo.Random(ctx, id)
	return modulesrpc.QuoteReply{Quote: view, Found: found}, err
}

func (q *quotesRPC) handleSearch(ctx context.Context, req modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	view, found, err := q.repo.Search(ctx, id, req.Text)
	return modulesrpc.QuoteReply{Quote: view, Found: found}, err
}

func (q *quotesRPC) handleEdit(ctx context.Context, req modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	createdAt, ok := parseQuoteDate(req.CreatedAt)
	if !ok {
		return modulesrpc.QuoteReply{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, "invalid quote date")}, nil
	}
	view, found, err := q.repo.Update(ctx, id, req.Number, repository.QuoteUpdate{Text: req.Text, CreatedAt: createdAt})
	return modulesrpc.QuoteReply{Quote: view, Found: found}, err
}

func (q *quotesRPC) handleRemove(ctx context.Context, req modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	found, err := q.repo.Remove(ctx, id, req.Number)
	return modulesrpc.QuoteReply{Found: found}, err
}

func (q *quotesRPC) handleList(ctx context.Context, _ modulesrpc.QuoteRequest, id uint64) (modulesrpc.QuoteReply, error) {
	quotes, err := q.repo.List(ctx, id)
	return modulesrpc.QuoteReply{Quotes: quotes}, err
}
