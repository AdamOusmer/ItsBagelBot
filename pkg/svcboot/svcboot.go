// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

const (
	appEnvProduction  = "production"
	appEnvDevelopment = "development"
	appEnvDebug       = "debug"
)

func resolveAppEnv(get func(string) string) (string, bool) {
	if value := get("APP_ENV"); value != "" {
		return value, false
	}
	return appEnvProduction, true
}

func FatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

type Core struct {
	Infra

	Log *zap.Logger
	NR  *newrelic.Application
	Ctx context.Context

	Service string
}

func NewLogger(serviceName string) *zap.Logger {
	appEnv, defaulted := resolveAppEnv(os.Getenv)
	log := logger.New(appEnv).Named(serviceName)
	if defaulted {
		log.Warn("APP_ENV not set; defaulted to production logging")
	}
	return log
}

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

// With the 10s preStop drain, must fit terminationGracePeriodSeconds (45s).
const rpcDrainTimeout = 15 * time.Second

func (c Core) Await() {
	<-c.Ctx.Done()
	c.Log.Info(c.Service + " shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), rpcDrainTimeout)
	defer cancel()
	if err := bus.DrainRPCHandlers(ctx); err != nil {
		c.Log.Warn("rpc handlers did not drain before the deadline", zap.Error(err))
	}
}

func MustValkey(core Core) valkey_go.Client {
	client, err := pkg_valkey.NewClient(core.ValkeyAddr, core.ValkeyPassword)
	FatalIf(core.Log, err, "failed to connect to valkey")
	return client
}

func MustRPCConn(core Core, url string) *nats.Conn {
	nc, err := bus.Connect(url, core.Service)
	FatalIf(core.Log, err, "failed to connect to nats")
	return nc
}

type NATS struct {
	URL       string
	RPCURL    string
	Pub       bus.Publisher
	RPC       *nats.Conn
	Broadcast bus.Subscriber
	Grouped   bus.Subscriber
}

func MustNATS(core Core) (NATS, func()) {
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
