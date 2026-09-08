// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package svcboot

import (
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/health"
)

// Health is one service's health identity: who it answers as, where it answers
// from, and on what.
//
// Service and QueueGroup travel in a struct for the same reason
// bus.RPCSubscription's do: both are strings and interchangeable at a call
// site, and a transposed pair registers a working responder on the wrong token,
// which shows up only as a sibling reading this service as down.
type Health struct {
	Log        *zap.Logger
	NC         *nats.Conn
	Service    string
	QueueGroup string
	// ListenAddr is where the HTTP surface binds; see Infra.ListenAddr.
	ListenAddr string
}

// NewHealthSet builds a service's health Set and attaches the RPC responder
// that answers out of it. Fatal on failure, matching the boot style.
//
// The NATS check is folded in rather than left to the caller because every one
// of the eleven call sites opened its check list with the identical
// health.NATS("nats", nc): a Set built on this connection that does not report
// on this connection is never the intent.
//
// One Set backs both surfaces on purpose: what a pod reports over HTTP at
// /status and what it replies over the health RPC can never disagree at the
// same instant. The ordering is load-bearing rather than stylistic — the
// responder can only be registered against a Set that already exists, and the
// check watching that registration only exists afterwards, which is why this is
// NewSet -> subscribe -> Add rather than one constructor call. That
// registration is killed asynchronously by a NATS permission violation while
// the pod keeps running and every other check stays green, so it needs its own
// check.
//
// Returned rather than served so a caller that owns its own HTTP server
// (app/db/transactions embeds the Set in its web handler) or needs a liveness
// check on it (app/discord/ingress's gateway session) can still use it. Callers
// with neither want ServeHealth.
func NewHealthSet(h Health, checks ...health.Check) *health.Set {
	set := health.NewSet(h.Service, append([]health.Check{health.NATS("nats", h.NC)}, checks...)...)

	rpcHealth, err := bus.SubscribeRPCHealth(h.NC, h.Service, h.QueueGroup, set)
	FatalIf(h.Log, err, "failed to subscribe rpc health")
	set.Add(rpcHealth)

	return set
}

// ServeHealth builds the Set and serves it on h.ListenAddr.
func ServeHealth(h Health, checks ...health.Check) {
	health.ServeSet(h.ListenAddr, NewHealthSet(h, checks...))
}
