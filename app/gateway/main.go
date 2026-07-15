// gateway is the fleet's external-API gateway: sesame (and any future caller)
// requests third-party data over NATS RPC and the gateway fetches, normalizes
// and caches it in Valkey.
//
// Its architecture mirrors sesame's: provider is the authoring surface,
// app/gateway/internal/providers holds one package per external system plus
// the one-line-per-provider All registration, and engine is the runtime that
// indexes and serves them. main only wires infrastructure — adding an external
// system never touches this file.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/gateway/internal/config"
	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/engine"
	"ItsBagelBot/app/gateway/internal/provider"
	"ItsBagelBot/app/gateway/internal/providers"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const serviceName = "gateway"

// queueGroup load-balances each endpoint across gateway replicas.
const queueGroup = "gateway-rpc"

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
		log.Warn("no providers configured; gateway will answer nothing")
	}
	if err := engine.Serve(nc, cfg.SubjectPrefix, queueGroup, active, nrApp, log); err != nil {
		log.Fatal("failed to subscribe provider endpoints", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, queueGroup); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}

	health.Serve(cfg.ListenAddr, nc.IsConnected)

	names := make([]string, 0, len(active))
	for _, p := range active {
		names = append(names, p.Name())
	}
	log.Info("gateway ready",
		zap.String("subject_prefix", cfg.SubjectPrefix),
		zap.Strings("providers", names),
	)

	<-ctx.Done()

	log.Info("gateway shutting down")
}
