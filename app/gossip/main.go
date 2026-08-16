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

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const serviceName = "gossip"

// queueGroup load-balances each endpoint across gossip replicas.
const queueGroup = "gossip-rpc"

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	// Deps is the bundle every provider captures; main builds it once.
	// providers.All returns the configured providers, which the engine
	// subscribes. Adding an external system is a new package under
	// internal/providers plus one line in all.go — no wiring here.
	//
	// GoveeKeys is the one provider dependency that needs the RPC connection:
	// the govee provider authenticates with each broadcaster's own key, fetched
	// just-in-time from the modules service. An empty subject prefix leaves it
	// nil, which disables the govee provider.
	deps := provider.Deps{
		Cache:   core.NewCache(core.NewValkeyStore(valkeyClient)),
		Limiter: ratelimit.New(valkeyClient),
		Log:     log,
	}
	if cfg.GoveeKeySubjectPrefix != "" {
		deps.GoveeKeys = core.NewGoveeKeyClient(nc, cfg.GoveeKeySubjectPrefix)
	}

	active := providers.All(cfg, deps)
	if len(active) == 0 {
		log.Warn("no providers configured; gossip will answer nothing")
	}
	if err := engine.Serve(nc, cfg.SubjectPrefix, queueGroup, active, nrApp, log); err != nil {
		log.Fatal("failed to subscribe provider endpoints", zap.Error(err))
	}
	subscribeRPCHealth(nc, queueGroup, log)

	health.Serve(cfg.ListenAddr, nc.IsConnected)

	names := make([]string, 0, len(active))
	for _, p := range active {
		names = append(names, p.Name())
	}
	log.Info("gossip ready",
		zap.String("subject_prefix", cfg.SubjectPrefix),
		zap.Strings("providers", names),
	)

	<-ctx.Done()

	log.Info("gossip shutting down")
	drainRPCHandlers(log)
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
