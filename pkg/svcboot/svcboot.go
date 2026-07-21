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
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	entsql "entgo.io/ent/dialect/sql"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

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
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)

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
// publisher, the core RPC connection (with the health responder attached), a
// broadcast subscriber (no queue group: every instance sees every message, for
// cache invalidation) and a durable group subscriber (exactly one instance
// handles each event).
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
	if err := bus.SubscribeRPCHealth(nc, serviceName, queueGroup); err != nil {
		core.Log.Fatal("failed to subscribe rpc health", zap.Error(err))
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
