// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package svcboot is the Facade over the service lifecycle for every Go binary
// under app/. It owns the plumbing each service used to repeat before its first
// line of real wiring: named logger, New Relic app, signal context, the shared
// endpoint block, the env-conventional MySQL driver, the standard NATS
// connections, the Valkey client and the health surface.
//
// The lifecycle is a Template Method: NewCore -> Must* connectors -> ServeHealth
// -> Await, in that fixed order, with each service supplying only its own
// wiring between the steps. main becomes wiring and nothing else.
//
// Keeping the skeleton here means a change to the boot conventions (bus
// constructor signatures, credential env names, observability wiring, the
// shutdown drain) lands in one file instead of once per service main. The
// twelve hand-rolled copies this replaced had already drifted: they defaulted
// APP_ENV to development, so a production pod that forgot to set it got
// verbose, unsampled logging with nothing saying so — see resolveAppEnv — and
// only one of the fifteen drained its RPC handlers before shutdown, see Await.
//
// pkg/svcboot/guard_test.go is what keeps the next main from hand-rolling it
// again.
package svcboot

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	valkey_go "github.com/valkey-io/valkey-go"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

// APP_ENV values logger.New understands.
const (
	appEnvProduction  = "production"
	appEnvDevelopment = "development"
	appEnvDebug       = "debug"
)

// resolveAppEnv resolves APP_ENV through get and reports whether it was
// defaulted. Defaults to production, not development (flipped 2026-08): a
// production boot that forgot APP_ENV used to silently get verbose, unsampled
// development logs — the expensive configuration — with nothing saying so. A
// missing value must fall to the restrictive end, and defaulted=true is what
// NewLogger turns into the one-time boot warning that says it happened.
func resolveAppEnv(get func(string) string) (string, bool) {
	if value := get("APP_ENV"); value != "" {
		return value, false
	}
	return appEnvProduction, true
}

// FatalIf aborts startup on err. A service cannot run degraded without any of
// its core dependencies, so a failed boot step must crash the pod and let
// Kubernetes restart it.
//
// It reads at the call site as what the failure does, which is why the boot
// path uses it in place of an if/Fatal block: three services had already
// written this same helper into their own main, byte for byte.
func FatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

// Core bundles the observability and lifecycle plumbing every service starts
// with: the named, New-Relic-wrapped logger, the APM app, the SIGINT/SIGTERM
// context main blocks on, and the shared endpoint block the connectors read.
type Core struct {
	// Infra is embedded so a service with no config package of its own reads
	// core.NATSURL and core.ListenAddr directly.
	Infra

	Log *zap.Logger
	NR  *newrelic.Application
	Ctx context.Context

	// Service is the fleet-wide service name: the logger's name, the New
	// Relic app, the NATS client name and the health RPC token, all of which
	// have to agree.
	Service string
}

// NewLogger builds the process logger from APP_ENV, warning once when the value
// was defaulted.
//
// Exported separately from NewCore for the one-shot cron entrypoints
// (app/db/notifications' `cleanup` argv mode) that fire a single RPC and exit:
// they need the logger and its APP_ENV decision, but starting an APM app for a
// process that lives sixty seconds would report a phantom service instance.
func NewLogger(serviceName string) *zap.Logger {
	appEnv, defaulted := resolveAppEnv(os.Getenv)
	log := logger.New(appEnv).Named(serviceName)
	if defaulted {
		// Once, before any wiring: after this point nothing else will say it.
		log.Warn("APP_ENV not set; defaulted to production logging")
	}
	return log
}

// NewCore boots the logger, the New Relic app and the signal context. The
// returned cleanup stops signal delivery, flushes the APM agent and syncs the
// logger, in that order; defer it first so it runs last.
func NewCore(serviceName string) (Core, func()) {
	log := NewLogger(serviceName)

	nrApp, err := monitor.New(serviceName, log)
	FatalIf(log, err, "failed to start new relic")
	log = monitor.WrapLogger(log, nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	cleanup := func() {
		stop()
		monitor.Shutdown(nrApp)
		_ = log.Sync()
	}
	return Core{Infra: LoadInfra(), Log: log, NR: nrApp, Ctx: ctx, Service: serviceName}, cleanup
}

// rpcDrainTimeout bounds the wait for in-flight RPC handlers at shutdown. It
// fits inside the pod's budget: the preStop hook holds SIGTERM for 10s on
// /drain and terminationGracePeriodSeconds is 45, so a handler blocked on its
// own 10-15s upstream timeout is still given room to answer before the kubelet
// escalates to SIGKILL.
const rpcDrainTimeout = 15 * time.Second

// Await blocks until SIGINT or SIGTERM, logs the shutdown line, and drains
// in-flight RPC handlers.
//
// The drain has to happen here, inside the last statement of main, rather than
// in a deferred close: handlers execute on pool workers rather than on the NATS
// callback goroutine, so a main that returns straight into its deferred closers
// shuts the NATS connection, the Valkey client and the database underneath a
// handler still using them. The requester loses its reply and the log fills
// with use-after-close noise that reads like a broker fault rather than a
// shutdown. Only app/gossip did this before; the other fourteen mains closed
// everything out from under their handlers.
//
// A service whose shutdown is more than this (an HTTP server to drain, a
// JetStream consumer's in-flight events to finish) keeps its own tail and does
// not call Await.
func (c Core) Await() {
	<-c.Ctx.Done()
	c.Log.Info(c.Service + " shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), rpcDrainTimeout)
	defer cancel()
	if err := bus.DrainRPCHandlers(ctx); err != nil {
		c.Log.Warn("rpc handlers did not drain before the deadline", zap.Error(err))
	}
}

// MustValkey opens the shared Valkey client from Infra. Fatal on failure: every
// caller either caches, rate-limits or holds lease state there and has no
// degraded mode without it.
func MustValkey(core Core) valkey_go.Client {
	client, err := pkg_valkey.NewClient(core.ValkeyAddr, core.ValkeyPassword)
	FatalIf(core.Log, err, "failed to connect to valkey")
	return client
}

// MustRPCConn opens one core request/reply connection to url, named for the
// service so the broker's connz output says who is holding it.
//
// url stays a parameter because the services genuinely disagree on it: the ones
// with a config package dial cfg.NATSRPCURL (NATS_RPC_URL, which may point at a
// different account or leaf), while the data tier derives it from NATS_URL with
// bus.RPCURL. Defaulting it here would silently move half the fleet's RPC
// plane.
func MustRPCConn(core Core, url string) *nats.Conn {
	nc, err := bus.Connect(url, core.Service)
	FatalIf(core.Log, err, "failed to connect to nats")
	return nc
}

// NATS bundles the standard connection set of a data service: the JetStream
// publisher, the core RPC connection, a broadcast subscriber (no queue group:
// every instance sees every message, for cache invalidation) and a durable
// group subscriber (exactly one instance handles each event).
//
// The health responder is not attached here — see NewHealthSet.
type NATS struct {
	URL    string
	RPCURL string
	Pub    bus.Publisher
	RPC    *nats.Conn
	// Broadcast fans every event out to every instance; Grouped delivers each
	// event to exactly one instance of the service's durable group.
	Broadcast bus.Subscriber
	Grouped   bus.Subscriber
}

// MustNATS opens the standard connection set. Fatal on any failure, matching
// the services' boot style. The returned closeIntake shuts the two subscribers
// and the RPC connection — the message intake — and is deliberately separate
// from Pub: main defers Pub.Close before its repository's Close so pending
// writes still flush through the publisher during shutdown.
func MustNATS(core Core) (NATS, func()) {
	// bus.RPCURL(NATS_URL), not core.NATSRPCURL: the data tier has never been
	// given a split RPC endpoint, and reading NATS_RPC_URL here would move it
	// the first time an unrelated service set that variable fleet-wide.
	rpcURL := bus.RPCURL(core.NATSURL)

	pub, err := bus.NewPublisher(core.NATSURL, core.Log)
	FatalIf(core.Log, err, "failed to connect publisher")

	nc := MustRPCConn(core, rpcURL)

	broadcast, err := bus.NewSubscriber(core.NATSURL, "", core.Log)
	FatalIf(core.Log, err, "failed to connect broadcast subscriber")

	grouped, err := bus.NewSubscriber(core.NATSURL, core.Service, core.Log)
	FatalIf(core.Log, err, "failed to connect group subscriber")

	n := NATS{URL: core.NATSURL, RPCURL: rpcURL, Pub: pub, RPC: nc, Broadcast: broadcast, Grouped: grouped}
	closeIntake := func() {
		_ = grouped.Close()
		_ = broadcast.Close()
		nc.Close()
	}
	return n, closeIntake
}
