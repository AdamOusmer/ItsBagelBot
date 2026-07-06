// gateway is the fleet's external-API gateway: sesame (and any future caller)
// requests third-party data over NATS RPC and the gateway fetches, normalizes
// and caches it in Valkey. External systems plug in as providers
// (app/gateway/internal/providers/*); adding one is a new package plus one
// line in buildProviders.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/gateway/internal/config"
	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	"ItsBagelBot/app/gateway/internal/providers/mcsr"
	"ItsBagelBot/app/gateway/internal/providers/urchin"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/nats-io/nats.go"
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

	cache := core.NewCache(core.NewValkeyStore(valkeyClient))
	limiter := ratelimit.New(valkeyClient)

	providers := buildProviders(cfg, cache, limiter, log)
	if len(providers) == 0 {
		log.Warn("no providers configured; gateway will answer nothing")
	}
	if err := subscribeProviders(nc, cfg.SubjectPrefix, providers, nrApp, log); err != nil {
		log.Fatal("failed to subscribe provider endpoints", zap.Error(err))
	}

	health.Serve(cfg.ListenAddr, nc.IsConnected)

	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name())
	}
	log.Info("gateway ready",
		zap.String("subject_prefix", cfg.SubjectPrefix),
		zap.Strings("providers", names),
	)

	<-ctx.Done()

	log.Info("gateway shutting down")
}

// buildProviders wires every configured provider. A provider missing its
// credentials is skipped with a warning: its subjects then simply time out at
// the caller, the same failure mode as the upstream being down.
func buildProviders(cfg *config.Config, cache *core.Cache, limiter *ratelimit.Limiter, log *zap.Logger) []provider.Provider {
	var providers []provider.Provider

	if cfg.UrchinAPIKey != "" {
		providers = append(providers, urchin.New(urchin.Config{
			BaseURL:   cfg.UrchinBaseURL,
			APIKey:    cfg.UrchinAPIKey,
			RateLimit: cfg.UrchinRateLimit,
		}, cache, limiter, log))
	} else {
		log.Warn("urchin provider disabled: URCHIN_API_KEY not set")
	}

	if cfg.McsrEnabled {
		providers = append(providers, mcsr.New(mcsr.Config{
			BaseURL:   cfg.McsrBaseURL,
			APIKey:    cfg.McsrAPIKey,
			RateLimit: cfg.McsrRateLimit,
		}, cache, limiter, log))
	} else {
		log.Warn("mcsr provider disabled: MCSR_ENABLED=false")
	}

	return providers
}

// subscribeProviders registers every endpoint of every provider on the RPC
// connection, queue-grouped so replicas share the load.
func subscribeProviders(nc *nats.Conn, prefix string, providers []provider.Provider, nrApp *newrelic.Application, log *zap.Logger) error {
	for _, p := range providers {
		for _, ep := range p.Endpoints() {
			subject := gatewayrpc.Subject(prefix, p.Name(), ep.Name)
			handle := ep.Handle
			err := bus.QueueSubscribeJSON(nc, subject, queueGroup, ep.Timeout, nrApp, log,
				func(ctx context.Context, req gatewayrpc.Request) any {
					return handle(ctx, req)
				})
			if err != nil {
				return err
			}
			log.Debug("gateway endpoint registered", zap.String("subject", subject))
		}
	}
	return nil
}
