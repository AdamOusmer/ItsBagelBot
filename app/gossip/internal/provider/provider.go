// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package provider is the gossip service's provider authoring surface, the twin of
// sesame's module package: one Provider wraps one external system (urchin,
// hypixel, mcsr, ...) and declares the RPC endpoints it answers; the engine
// (app/gossip/internal/engine) indexes and serves them at
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

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
)

// HandlerFunc answers one RPC request, returning the reply value to marshal
// back. It must embed the conventional {"error": ""} envelope and report
// user-facing failures (player not found) there rather than panicking or
// returning nothing. Pre-marshaled bytes (json.RawMessage) pass to the wire
// untouched.
type HandlerFunc func(ctx context.Context, req gossiprpc.Request) any

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

// BroadcasterKeyResolver hands a provider the decrypted per-broadcaster
// credential for one external system — govee's API key, spotify's OAuth
// refresh token — resolved from the modules service over an internal RPC. It
// is the gossip service's twin of outgress's tokenstore: the service that
// dials the upstream fetches the sealed credential just-in-time instead of
// holding a copy. An empty key with a nil error means the broadcaster has
// none on file (not set up), which the providers report as a friendly reply
// error.
type BroadcasterKeyResolver interface {
	Key(ctx context.Context, broadcasterID string) (string, error)
}

// SpotifyCredResolver hands the spotify provider one broadcaster's whole
// credential set, resolved just-in-time from the modules service over the
// internal RPC — the same custody posture as BroadcasterKeyResolver, widened
// because Spotify needs three values rather than one and a second round trip
// per chat command is not worth the narrower signature. The credential struct
// lives in core because the resolver that implements this does too, and core
// cannot import this package.
type SpotifyCredResolver interface {
	Credentials(ctx context.Context, broadcasterID string) (core.SpotifyCredentials, error)
}

// FetchKeyResolver resolves a broadcaster's stored API key BY LABEL for the
// custom urlfetch provider — the commands-service twin of GoveeKeyClient.
// Same custody rules: the plaintext rides one fetch and is never cached,
// logged, or projected anywhere. An empty key with a nil error means none is
// on file for that label, which callers must treat as fail-closed.
type FetchKeyResolver interface {
	FetchKey(ctx context.Context, broadcasterID, label string) (string, error)
}

// DefSource is the read seam for projected urlfetch definitions — lane A's
// projection Client.FetchDefs (tier-1 in-process entry, tier-2 Valkey,
// tier-3 projector-RPC fallback), declared here as a minimal interface so
// gossip builds against the contract, not the concrete client. found is false
// for a name with no row; an inactive def IS found (IsActive false) and the
// caller maps it to bad_def.
//
// SEAM NOTE (recorded deliberately): internal/projection.Client.FetchDefs had
// not landed when this lane built; main.go wires an adapter here once it
// does, and this interface stays the contract either way.
type DefSource interface {
	FetchDef(ctx context.Context, broadcasterID, name string) (gossiprpc.FetchDef, bool, error)
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
	GoveeKeys BroadcasterKeyResolver
	// SpotifyKeys resolves per-broadcaster Spotify credentials for the spotify
	// provider, under the same custody split as GoveeKeys. It resolves more
	// than a key because a broadcaster now owns the whole chain: their own
	// registered application AND the grant minted against it.
	SpotifyKeys SpotifyCredResolver
	// FetchKeys resolves per-broadcaster API keys by label for the custom
	// urlfetch provider. Optional: keyless definitions still work when nil —
	// only defs carrying a key_label then answer bad_def (fail closed).
	FetchKeys FetchKeyResolver
	// FetchDefs reads projected fetch definitions for the custom urlfetch
	// provider. nil disables the whole provider: with no definition source
	// there is nothing to fetch.
	FetchDefs DefSource
}

// Logger returns Log, or a nop logger when it is unset, so providers and the
// Builder never nil-check it themselves.
func (d Deps) Logger() *zap.Logger {
	if d.Log == nil {
		return zap.NewNop()
	}
	return d.Log
}
