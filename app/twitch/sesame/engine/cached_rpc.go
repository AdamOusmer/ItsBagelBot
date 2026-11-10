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
	decode  func(Rep) (Res, time.Duration, string)
}

func newCachedRPC[Req, Rep, Res any](
	results *cache.Cache[Res],
	request func(context.Context, Req) (Rep, error),
	decode func(Rep) (Res, time.Duration, string),
) *cachedRPC[Req, Rep, Res] {
	return &cachedRPC[Req, Rep, Res]{cache: results, request: request, decode: decode}
}

func newBusRPC[Req, Rep, Res any](nc *nats.Conn, ep rpcEndpoint, decode func(Rep) (Res, time.Duration, string)) *cachedRPC[Req, Rep, Res] {
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
		var zero Res
		reply, err := c.request(ctx, req)
		if err != nil {
			return zero, 0, err
		}
		result, ttl, replyErr := c.decode(reply)
		if replyErr != "" {
			return zero, 0, errors.New(replyErr)
		}
		return result, ttl, nil
	})
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
