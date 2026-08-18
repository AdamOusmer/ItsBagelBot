// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// gossip is the fleet's external-API gateway: sesame (and any future caller)
// requests third-party data over NATS RPC and gossip fetches, normalizes
// and caches it in Valkey.
//
// Its architecture mirrors sesame's: provider is the authoring surface,
// app/gossip/internal/providers holds one package per external system plus
// the one-line-per-provider All registration, and engine is the runtime that
// indexes and serves them. main only wires infrastructure — adding an external
// system never touches this file.
package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/engine"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/AdamOusmer/recipes/runtime"
	"github.com/AdamOusmer/recipes/svc/zwmhl"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const serviceName = "gossip"

// queueGroup load-balances each endpoint across gossip replicas.
const queueGroup = "gossip-rpc"

// fatalIf aborts startup on err: gossip cannot run degraded without any of
// its core dependencies, so a failed step must crash the pod for Kubernetes
// to restart it.
func fatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	fatalIf(log, err, "failed to start new relic")
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	fatalIf(log, err, "failed to connect to valkey")
	defer valkeyClient.Close()

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	fatalIf(log, err, "failed to connect to nats")
	defer nc.Close()

	busConn, k, grantsDenied := connectRecipes(ctx, cfg.NATSURL, nc, log)
	defer busConn.Close()

	// Deps is the bundle every provider captures; main builds it once.
	// providers.All returns the configured providers, which the engine
	// subscribes. Adding an external system is a new package under
	// internal/providers plus one line in all.go — no wiring here.
	//
	// GoveeKeys is the one provider dependency that needs the RPC connection:
	// the govee provider authenticates with each broadcaster's own key, fetched
	// just-in-time from the modules service. GoveeEnabled (not an empty subject
	// prefix — the recipes grant is always the same concrete subject) is the
	// kill switch that leaves it nil and disables the govee provider.
	deps := provider.Deps{
		Cache:   core.NewCache(core.NewValkeyStore(valkeyClient)),
		Limiter: ratelimit.New(valkeyClient),
		Log:     log,
	}
	if cfg.GoveeEnabled {
		deps.GoveeKeys = core.NewGoveeKeyClient(nc, strings.TrimSuffix(k.ZUHT6(), ".>"))
	}

	active := providers.All(cfg, deps)
	if len(active) == 0 {
		log.Warn("no providers configured; gossip will answer nothing")
	}
	fatalIf(log, engine.Serve(nc, strings.TrimSuffix(k.ZQMXJ(), ".>"), queueGroup, active, nrApp, log),
		"failed to subscribe provider endpoints")
	subscribeRPCHealth(nc, queueGroup, log)

	health.Serve(cfg.ListenAddr, serviceName,
		health.Bool("nats", nc.IsConnected),
		health.Bool("nats_grants", func() bool { return !grantsDenied.Load() }),
	)

	names := make([]string, 0, len(active))
	for _, p := range active {
		names = append(names, p.Name())
	}
	log.Info("gossip ready",
		zap.String("subject_prefix", strings.TrimSuffix(k.ZQMXJ(), ".>")),
		zap.Strings("providers", names),
	)

	<-ctx.Done()

	log.Info("gossip shutting down")
	drainRPCHandlers(log)
}

// connectRecipes dials the BUS-plane connection, binds gossip's recipes
// binding over it and nc, wires its permission-violation Watchdog, and
// preflights the (empty — gossip touches no JetStream stream) grant set it
// declares. busConn exists only to satisfy Up(U): U.Bus and carry the
// Watchdog. The returned pointer backs the nats_grants health check.
func connectRecipes(ctx context.Context, natsURL string, nc *nats.Conn, log *zap.Logger) (*nats.Conn, *zwmhl.K, *atomic.Bool) {
	busConn, err := bus.ConnectBus(natsURL, serviceName)
	fatalIf(log, err, "failed to connect bus-plane nats")

	k, err := zwmhl.Up(zwmhl.U{Bus: busConn, Rpc: nc})
	fatalIf(log, err, "failed to bind gossip's recipes binding")

	// grantsDenied flips true the first time the NATS server reports gossip's
	// BUS account denied a permission its manifest declares (see
	// runtime.Watchdog).
	grantsDenied := &atomic.Bool{}
	watchdog := runtime.NewWatchdog(k.M(), func(subject, canonical string, err error) {
		log.Error("nats permission violation",
			zap.String("subject", subject), zap.String("canonical", canonical), zap.Error(err))
	}, func() { grantsDenied.Store(true) })
	bus.GuardConnection(busConn, watchdog.Handler())

	fatalIf(log, runtime.PreflightStreams(ctx, busConn, k.Expectations(), 0),
		"recipes preflight failed: missing jetstream grant(s)")

	return busConn, k, grantsDenied
}

// rpcDrainTimeout bounds the wait for in-flight RPC handlers at shutdown. It
// fits inside the pod's budget: the preStop hook holds SIGTERM for 10s on
// /drain and terminationGracePeriodSeconds is 45, so a handler blocked on its
// own 10-15s upstream timeout is still given room to answer before the kubelet
// escalates to SIGKILL.
const rpcDrainTimeout = 15 * time.Second

// drainRPCHandlers waits for handlers that are mid-request before main returns
// and its deferred closes run. Handlers now execute on pool workers rather than
// on the NATS callback goroutine, so without this a SIGTERM would close the NATS
// connection and the Valkey client underneath a handler still using them: the
// requester loses its reply and the log fills with use-after-close noise that
// looks like a broker fault rather than a shutdown.
func drainRPCHandlers(log *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), rpcDrainTimeout)
	defer cancel()
	if err := bus.DrainRPCHandlers(ctx); err != nil {
		log.Warn("rpc handlers did not drain before the deadline", zap.Error(err))
	}
}

func subscribeRPCHealth(nc *nats.Conn, queueGroup string, log *zap.Logger) {
	if err := bus.SubscribeRPCHealth(nc, serviceName, queueGroup); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}
}
