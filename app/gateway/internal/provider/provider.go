// Package provider is the gateway's provider authoring surface, the twin of
// sesame's module package: one Provider wraps one external system (urchin,
// hypixel, mcsr, ...) and declares the RPC endpoints it answers; the engine
// (app/gateway/internal/engine) indexes and serves them at
// "<prefix>.<provider>.<endpoint>". Adding an external system is a new package
// under internal/providers plus one line in providers.All — the same shape as
// sesame's modules.All.
//
// Like sesame's module package, this one carries no runtime wiring: a provider
// captures the services it needs (cache, limiter, HTTP clients) from Deps by
// closure, so the authoring surface stays small and unit-testable on its own.
package provider

import (
	"context"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// Endpoint is one RPC verb a provider answers. Handle returns the reply value
// to marshal back; it must embed the conventional {"error": ""} envelope and
// report user-facing failures (player not found) there rather than panicking
// or returning nothing.
type Endpoint struct {
	// Name is the last subject token ("daily", "user", "session_start", ...).
	Name string
	// Timeout bounds one handler run; zero means the bus default (5s).
	Timeout time.Duration
	// Handle answers one request.
	Handle func(ctx context.Context, req gatewayrpc.Request) any
}

// Provider is one external API system.
type Provider interface {
	// Name is the subject token identifying the system ("urchin", "mcsr").
	Name() string
	// Endpoints lists the verbs the provider answers.
	Endpoints() []Endpoint
}

// Deps is the bundle of runtime services a provider captures when it is built,
// mirroring sesame's engine.Deps: main constructs it once and hands it to
// providers.All. Not every provider uses every field; unused ones are harmless.
type Deps struct {
	Cache   *core.Cache
	Limiter *ratelimit.Limiter
	Log     *zap.Logger
}
