// Package provider is the gateway's provider authoring surface, the twin of
// sesame's module package: one Provider wraps one external system (urchin,
// hypixel, mcsr, ...) and declares the RPC endpoints it answers; the engine
// (app/gateway/internal/engine) indexes and serves them at
// "<prefix>.<provider>.<endpoint>". Adding an external system is a new package
// under internal/providers plus one line in providers.All — the same shape as
// sesame's modules.All.
//
// A provider is declared through the fluent Builder (see NewProvider), the
// twin of sesame's module.Builder: endpoints chain their timeout and terminal
// handler, and Build returns the immutable Provider the engine consumes.
// Bespoke endpoints capture the services they need (limiter, HTTP clients) by
// closure; the cached fetch-and-shape skeleton every stats endpoint shares is
// declared once through the FlowBuilder instead of hand-rolled per endpoint.
package provider

import (
	"context"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// HandlerFunc answers one RPC request, returning the reply value to marshal
// back. It must embed the conventional {"error": ""} envelope and report
// user-facing failures (player not found) there rather than panicking or
// returning nothing. Pre-marshaled bytes (json.RawMessage) pass to the wire
// untouched.
type HandlerFunc func(ctx context.Context, req gatewayrpc.Request) any

// Endpoint is one RPC verb a provider answers.
type Endpoint struct {
	// Name is the last subject token ("daily", "user", "session_start", ...).
	Name string
	// Timeout bounds one handler run; zero means the bus default (5s).
	Timeout time.Duration
	// Handle answers one request.
	Handle HandlerFunc
}

// Provider is one external API system.
type Provider interface {
	// Name is the subject token identifying the system ("urchin", "mcsr").
	Name() string
	// Endpoints lists the verbs the provider answers.
	Endpoints() []Endpoint
}

// GoveeKeyResolver hands the govee provider the decrypted Govee API key for one
// broadcaster, resolved from the modules service over an internal RPC. It is
// the gateway's twin of outgress's tokenstore: the service that dials the
// upstream fetches the sealed credential just-in-time instead of holding a
// copy. An empty key with a nil error means the broadcaster has none on file
// (govee not set up), which the provider reports as a friendly reply error.
type GoveeKeyResolver interface {
	Key(ctx context.Context, broadcasterID string) (string, error)
}

// Deps is the bundle of runtime services a provider captures when it is built,
// mirroring sesame's engine.Deps: main constructs it once and hands it to
// providers.All. Not every provider uses every field; unused ones are harmless.
type Deps struct {
	Cache   *core.Cache
	Limiter *ratelimit.Limiter
	Log     *zap.Logger
	// GoveeKeys resolves per-broadcaster Govee API keys for the govee provider.
	// nil disables that provider (providers.All skips it), the same degrade as a
	// missing service API key.
	GoveeKeys GoveeKeyResolver
}

// Logger returns Log, or a nop logger when it is unset, so providers and the
// Builder never nil-check it themselves.
func (d Deps) Logger() *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}
