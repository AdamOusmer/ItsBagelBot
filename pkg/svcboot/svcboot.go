// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package svcboot is the shared boot scaffold for the fleet's Go data services
// (commands, modules, ...). It owns the plumbing every service repeats before
// its first line of real wiring: named logger, New Relic app, signal context,
// the env-conventional MySQL driver and the standard set of NATS connections.
// Keeping it here means a change to the boot conventions (bus constructor
// signatures, credential env names, observability wiring) lands in one file
// instead of once per service main.
package svcboot

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

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
// NewCore turns into the one-time boot warning that says it happened.
func resolveAppEnv(get func(string) string) (string, bool) {
	if value := get("APP_ENV"); value != "" {
		return value, false
	}
	return appEnvProduction, true
}

// Core bundles the observability and lifecycle plumbing every service starts
// with: the named, New-Relic-wrapped logger, the APM app and the SIGINT/SIGTERM
// context main blocks on.
type Core struct {
	Log *zap.Logger
	NR  *newrelic.Application
	Ctx context.Context
}

// NewCore boots the logger, the New Relic app and the signal context. The
// returned cleanup stops signal delivery, flushes the APM agent and syncs the
// logger, in that order; defer it first so it runs last.
func NewCore(serviceName string) (Core, func()) {
	appEnv, defaulted := resolveAppEnv(os.Getenv)
	log := logger.New(appEnv).Named(serviceName)
	if defaulted {
		// Once, before any wiring: after this point nothing else will say it.
		log.Warn("APP_ENV not set; defaulted to production logging")
	}

	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
	log = monitor.WrapLogger(log, nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	cleanup := func() {
		stop()
		monitor.Shutdown(nrApp)
		_ = log.Sync()
	}
	return Core{Log: log, NR: nrApp, Ctx: ctx}, cleanup
}

// MustEntDriver opens the MySQL driver from the fleet's env conventions
// (DB_ADDR, DB_USER, DB_PASS, DB_SCHEMA). Fatal on failure: a data service
// without its database can only crashloop later anyway.
func MustEntDriver(log *zap.Logger, defaultSchema string) *entsql.Driver {
	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", defaultSchema),
	})
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}
	return driver
}

// AutoMigrate runs the service's ent auto-migration unless disabled by
// DB_AUTO_MIGRATE. The generated ent clients are distinct types per service,
// so the schema-create step arrives as a closure (client.Schema.Create).
func AutoMigrate(ctx context.Context, log *zap.Logger, create func(context.Context) error) {
	if !env.GetBool("DB_AUTO_MIGRATE", true) {
		return
	}
	if err := create(ctx); err != nil {
		log.Fatal("failed to run migrations", zap.Error(err))
	}
}

// NATS bundles the standard connection set of a data service: the JetStream
// publisher, the core RPC connection, a broadcast subscriber (no queue group:
// every instance sees every message, for cache invalidation) and a durable
// group subscriber (exactly one instance handles each event).
//
// The health responder is not attached here — see MustHealthRPC.
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
func MustNATS(core Core, serviceName, queueGroup string) (NATS, func()) {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	pub, err := bus.NewPublisher(natsURL, core.Log)
	if err != nil {
		core.Log.Fatal("failed to connect publisher", zap.Error(err))
	}

	nc, err := bus.Connect(rpcURL, serviceName)
	if err != nil {
		core.Log.Fatal("failed to connect to nats", zap.Error(err))
	}
	broadcast, err := bus.NewSubscriber(natsURL, "", core.Log)
	if err != nil {
		core.Log.Fatal("failed to connect broadcast subscriber", zap.Error(err))
	}

	grouped, err := bus.NewSubscriber(natsURL, serviceName, core.Log)
	if err != nil {
		core.Log.Fatal("failed to connect group subscriber", zap.Error(err))
	}

	n := NATS{URL: natsURL, RPCURL: rpcURL, Pub: pub, RPC: nc, Broadcast: broadcast, Grouped: grouped}
	closeIntake := func() {
		_ = grouped.Close()
		_ = broadcast.Close()
		nc.Close()
	}
	return n, closeIntake
}

// ServeDataHealth builds a data-tier service's health surface, attaches the RPC
// responder that answers out of it, and serves it. Fatal on failure, matching
// MustNATS. Every service behind health.itsbagelbot.com/db calls this, so the
// five of them cannot drift into reporting different things.
//
// One Set backs both surfaces on purpose: the aggregate the projector serves at
// /db can never disagree with what a pod reports over HTTP at the same instant.
// The responder can only be registered against a Set that already exists, and
// the check watching that registration only exists afterwards, so the ordering
// here is load-bearing rather than stylistic.
//
// The mysql check sits alongside nats because PingContext exercises the same
// pool the repository code uses, catching a wedged pool or rotated-out
// credentials that IsConnected alone would miss (pkg/db/health.go). It degrades
// rather than fails readiness: a hard failure would pull every pod of a service
// out of rotation on one shared DB blip, turning a brief outage into a total
// one. A healthy ping lands in single-digit ms (measured ~3.6ms pod-to-MySQL
// RTT); much higher means the pool went cold and is paying the ~18ms handshake
// instead of reusing a connection.
//
// The database check stays with each service rather than being hoisted into the
// projector's /db aggregate: the schemas are expected to split across servers,
// and one hoisted check could not name which one went.
//
// extra carries whatever else a given service depends on — a lane check for the
// ones that consume a durable group, nothing for the request/reply-only ones.
// It is a parameter rather than a nil-able subscriber because bus.LaneCheck on a
// nil Subscriber silently passes, which is worse than having no check at all.
func ServeDataHealth(d DataHealth, extra ...health.Check) {
	checks := append([]health.Check{health.NATS("nats", d.NC)}, extra...)
	set := health.NewSet(d.Service, append(checks, health.Degrades(db.HealthCheck("mysql", d.Pool)))...)

	rpcHealth, err := bus.SubscribeRPCHealth(d.NC, d.Service, d.QueueGroup, set)
	if err != nil {
		d.Log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}
	set.Add(rpcHealth)

	health.ServeSet(env.Get("LISTEN_ADDR", ":8080"), set)
}

// DataHealth is one data-tier service's identity and dependencies, travelling
// as a single value for the same reason bus.RPCSubscription does: Service and
// QueueGroup are both strings and interchangeable at a call site, and a
// transposed pair registers a working responder on the wrong token, which shows
// up only as a sibling reading this service as down.
type DataHealth struct {
	Log        *zap.Logger
	NC         *nats.Conn
	Service    string
	QueueGroup string
	Pool       *sql.DB
}
