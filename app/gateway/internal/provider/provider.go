// Package provider defines the gateway's pluggable external-API surface. One
// Provider wraps one external system (urchin, mcsr, ...) and declares the RPC
// endpoints it answers; main subscribes each endpoint at
// "<prefix>.<provider>.<endpoint>" with the shared queue group. Adding an
// external system is a new package under internal/providers plus one line in
// main — the same shape as sesame's module registry.
package provider

import (
	"context"
	"time"

	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
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
