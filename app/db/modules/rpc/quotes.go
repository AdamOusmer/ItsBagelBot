// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/modules/repository"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"ItsBagelBot/pkg/bus"
)

type quotesRPC struct {
	repo *repository.Quotes
	log  *zap.Logger
}

// SubscribeQuotes answers the channel-quotes verbs under prefix (default
// "bagel.rpc.modules.quote"): add, get, random, search, edit, remove, list.
// They ride the same MODULES_RPC account export as the dashboard verbs, so no
// ACL change is needed for sesame to call them.
//
// The quote book is its own store, so the repository travels beside the shared
// wiring instead of inside the modules Wiring: bundling it there would mean a
// second repo field that only this one entry point ever reads.
func SubscribeQuotes(w bus.RPCWiring, repo *repository.Quotes, prefix string) error {
	q := &quotesRPC{repo: repo, log: w.Log}

	// VerbForUser, not At: the broadcaster-id guard is the whole prologue of
	// all seven handlers. It used to be a package-local withUserID closure
	// wrapper plus an errReply helper; ForUser is both, shared with every
	// other user-scoped verb in the fleet, so a returned error becomes the
	// reply's error field without either of them.
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

// quoteVerb names one guarded quote verb. It fixes the two type arguments
// every entry of the table would otherwise repeat.
func quoteVerb(name string, load func(context.Context, modulesrpc.QuoteRequest, uint64) (modulesrpc.QuoteReply, error)) bus.Verb[modulesrpc.QuoteRequest, modulesrpc.QuoteReply] {
	return bus.VerbForUser[modulesrpc.QuoteRequest, modulesrpc.QuoteReply](name, load)
}

// parseQuoteDate parses the optional RFC 3339 date riding an add/edit
// request; ok=false means it was present but malformed. An absent date stays
// the zero time (add stamps now, edit keeps the saved date).
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
		return modulesrpc.QuoteReply{Error: "invalid quote date"}, nil
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
		return modulesrpc.QuoteReply{Error: "invalid quote date"}, nil
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
