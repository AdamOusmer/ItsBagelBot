// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const quotesRPCTimeout = 2 * time.Second

type QuotesRPC struct {
	nc     *nats.Conn
	prefix string
}

func NewQuotesRPC(nc *nats.Conn, modulesPrefix string) *QuotesRPC {
	return &QuotesRPC{nc: nc, prefix: modulesPrefix + ".quote"}
}

func (c *QuotesRPC) call(ctx context.Context, verb string, req modulesrpc.QuoteRequest) (modulesrpc.QuoteReply, error) {
	reply, err := bus.RequestJSONTimeout[modulesrpc.QuoteReply](ctx, c.nc, c.prefix+"."+verb, req, quotesRPCTimeout)
	if err != nil {
		return modulesrpc.QuoteReply{}, err
	}
	if reply.Error != "" {
		return modulesrpc.QuoteReply{}, errors.New(reply.Error)
	}
	return reply, nil
}

func (c *QuotesRPC) QuoteAdd(ctx context.Context, broadcasterID uint64, text, addedBy string) (modulesrpc.Quote, error) {
	reply, err := c.call(ctx, "add", modulesrpc.QuoteRequest{
		UserID:  strconv.FormatUint(broadcasterID, 10),
		Text:    text,
		AddedBy: addedBy,
	})
	if err != nil {
		return modulesrpc.Quote{}, err
	}
	if reply.Quote == nil {
		return modulesrpc.Quote{}, errors.New("quote add: empty reply")
	}
	return *reply.Quote, nil
}

func (c *QuotesRPC) QuoteGet(ctx context.Context, broadcasterID, number uint64) (modulesrpc.Quote, bool, error) {
	reply, err := c.call(ctx, "get", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
		Number: number,
	})
	return foundQuote(reply, err)
}

func (c *QuotesRPC) QuoteRandom(ctx context.Context, broadcasterID uint64) (modulesrpc.Quote, bool, error) {
	reply, err := c.call(ctx, "random", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
	})
	return foundQuote(reply, err)
}

func foundQuote(reply modulesrpc.QuoteReply, err error) (modulesrpc.Quote, bool, error) {
	if err != nil {
		return modulesrpc.Quote{}, false, err
	}
	if reply.Quote == nil {
		return modulesrpc.Quote{}, false, nil
	}
	return *reply.Quote, reply.Found, nil
}

func (c *QuotesRPC) QuoteSearch(ctx context.Context, broadcasterID uint64, term string) (modulesrpc.Quote, bool, error) {
	reply, err := c.call(ctx, "search", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
		Text:   term,
	})
	return foundQuote(reply, err)
}

func (c *QuotesRPC) QuoteEdit(ctx context.Context, broadcasterID, number uint64, text string) (modulesrpc.Quote, bool, error) {
	reply, err := c.call(ctx, "edit", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
		Number: number,
		Text:   text,
	})
	return foundQuote(reply, err)
}

func (c *QuotesRPC) QuoteRemove(ctx context.Context, broadcasterID, number uint64) (bool, error) {
	reply, err := c.call(ctx, "remove", modulesrpc.QuoteRequest{
		UserID: strconv.FormatUint(broadcasterID, 10),
		Number: number,
	})
	if err != nil {
		return false, err
	}
	return reply.Found, nil
}
