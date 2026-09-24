// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordrate

import (
	"context"
	"errors"

	"ItsBagelBot/pkg/ratelimit"

	"github.com/valkey-io/valkey-go"
)

const (
	globalCapacity      = 45.0
	globalWindowSeconds = 1.0

	globalKey = "ratelimit:discord:global"
)

var globalSpec = ratelimit.NewSpec(globalCapacity, globalCapacity/globalWindowSeconds)

var ErrRateLimited = errors.New("dingress: discord global rate limit")

type Gate interface {
	Take(ctx context.Context) error
}

type Limiter struct {
	client *ratelimit.Limiter
}

func New(client valkey.Client) *Limiter {
	return &Limiter{client: ratelimit.New(client)}
}

func (l *Limiter) Take(ctx context.Context) error {
	allowed, err := l.client.Allow(ctx, globalSpec.ForKey(globalKey))
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}
