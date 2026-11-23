// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
)

type rpcEndpoint struct {
	subject  string
	timeout  time.Duration
	capacity int64
	missTTL  time.Duration
}

func rpcSubject(prefix, name string) string {
	return strings.TrimSuffix(prefix, ".") + "." + name + ".get"
}

type cachedRPC[Req, Rep, Res any] struct {
	cache   *cache.Cache[Res]
	request func(context.Context, Req) (Rep, error)
	decode  func(Rep) (Res, time.Duration, error)
}

func newCachedRPC[Req, Rep, Res any](
	results *cache.Cache[Res],
	request func(context.Context, Req) (Rep, error),
	decode func(Rep) (Res, time.Duration, error),
) *cachedRPC[Req, Rep, Res] {
	return &cachedRPC[Req, Rep, Res]{cache: results, request: request, decode: decode}
}

func newBusRPC[Req, Rep, Res any](nc *nats.Conn, ep rpcEndpoint, decode func(Rep) (Res, time.Duration, error)) *cachedRPC[Req, Rep, Res] {
	return newCachedRPC(
		cache.New[Res](ep.capacity, ep.missTTL),
		func(ctx context.Context, req Req) (Rep, error) {
			return bus.RequestJSONTimeout[Rep](ctx, nc, ep.subject, req, ep.timeout)
		},
		decode,
	)
}

func (c *cachedRPC[Req, Rep, Res]) lookup(ctx context.Context, key string, req Req) (Res, error) {
	return c.cache.GetOrLoadTTL(ctx, key, func(ctx context.Context) (Res, time.Duration, error) {
		return c.load(ctx, req)
	})
}

func (c *cachedRPC[Req, Rep, Res]) load(ctx context.Context, req Req) (Res, time.Duration, error) {
	reply, err := c.request(ctx, req)
	if err != nil {
		var zero Res
		return zero, 0, err
	}
	return c.decode(reply)
}

type RetryAfterError struct {
	After time.Duration
}

func (e *RetryAfterError) Error() string {
	return "lookup rate limited, retry after " + e.After.String()
}

func (e *RetryAfterError) RetryAfter() time.Duration { return e.After }

func lookupReplyError(message string, retryAtUnixMilli int64) error {
	if retryAtUnixMilli != 0 {
		return &RetryAfterError{After: max(0, time.Until(time.UnixMilli(retryAtUnixMilli)))}
	}
	return rpcReplyError(message)
}

func rpcReplyError(message string) error {
	if message != "" {
		return errors.New(message)
	}
	return nil
}

func ttlWhen(ok bool, positive, negative time.Duration) time.Duration {
	if ok {
		return positive
	}
	return negative
}

func targetCacheKey(targetID, targetLogin string) string {
	if targetID != "" {
		return targetID
	}
	return "login:" + strings.ToLower(strings.TrimPrefix(targetLogin, "@"))
}
