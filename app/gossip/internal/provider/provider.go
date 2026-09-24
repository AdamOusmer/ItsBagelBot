// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package provider

import (
	"context"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

type HandlerFunc func(ctx context.Context, req gossiprpc.Request) any

type Endpoint struct {
	Name    string
	Timeout time.Duration
	Handle  HandlerFunc
}

type Provider interface {
	Name() string
	Endpoints() []Endpoint
}

type BroadcasterKeyResolver interface {
	Key(ctx context.Context, broadcasterID string) (string, error)
}

type SpotifyCredResolver interface {
	Credentials(ctx context.Context, broadcasterID string) (core.SpotifyCredentials, error)
}

type FetchKeyResolver interface {
	FetchKey(ctx context.Context, broadcasterID, label string) (string, error)
}

type DefSource interface {
	FetchDef(ctx context.Context, broadcasterID, name string) (gossiprpc.FetchDef, bool, error)
}

type Deps struct {
	Cache       *core.Cache
	Limiter     *ratelimit.Limiter
	Log         *zap.Logger
	GoveeKeys   BroadcasterKeyResolver
	SpotifyKeys SpotifyCredResolver
	FetchKeys   FetchKeyResolver
	FetchDefs   DefSource
}

func (d Deps) Logger() *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}
