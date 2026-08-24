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
	"strconv"
	"syscall"
	"time"

	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/engine"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	valkey_go "github.com/valkey-io/valkey-go"
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

	valkeyClient := connectValkey(cfg, log)
	defer valkeyClient.Close()

	nc := connectNATS(cfg, log)
	defer nc.Close()

	deps := buildDeps(cfg, nc, valkeyClient, log)

	active := providers.All(cfg, deps)
	if len(active) == 0 {
		log.Warn("no providers configured; gossip will answer nothing")
	}
	if err := engine.Serve(nc, cfg.SubjectPrefix, queueGroup, active, nrApp, log); err != nil {
		log.Fatal("failed to subscribe provider endpoints", zap.Error(err))
	}
	subscribeRPCHealth(nc, queueGroup, log)

	health.Serve(cfg.ListenAddr, serviceName, health.NATS("nats", nc))

	logReady(active, cfg, log)

	awaitShutdown(ctx, log)
}

func connectValkey(cfg *config.Config, log *zap.Logger) valkey_go.Client {
	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	return valkeyClient
}

func connectNATS(cfg *config.Config, log *zap.Logger) *nats.Conn {
	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	return nc
}

func buildDeps(cfg *config.Config, nc *nats.Conn, valkeyClient valkey_go.Client, log *zap.Logger) provider.Deps {
	// Deps is the bundle every provider captures; main builds it once.
	// providers.All returns the configured providers, which the engine
	// subscribes. Adding an external system is a new package under
	// internal/providers plus one line in all.go — no wiring here.
	deps := provider.Deps{
		Cache:   core.NewCache(core.NewValkeyStore(valkeyClient)),
		Limiter: ratelimit.New(valkeyClient),
		Log:     log,
	}
	wireKeyResolvers(cfg, nc, valkeyClient, &deps)
	return deps
}

func logReady(active []provider.Provider, cfg *config.Config, log *zap.Logger) {
	names := make([]string, 0, len(active))
	for _, p := range active {
		names = append(names, p.Name())
	}
	log.Info("gossip ready",
		zap.String("subject_prefix", cfg.SubjectPrefix),
		zap.Strings("providers", names),
	)
}

func awaitShutdown(ctx context.Context, log *zap.Logger) {
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

// wireKeyResolvers attaches the just-in-time credential resolvers to deps —
// the provider dependencies that need the RPC connection. Govee
// authenticates with each broadcaster's own API key and spotify with their
// connected account's OAuth refresh token, both fetched from the modules
// service at call time. An empty subject prefix leaves a resolver nil, which
// disables its provider (providers.All skips it).
func wireKeyResolvers(cfg *config.Config, nc *nats.Conn, valkeyClient valkey_go.Client, deps *provider.Deps) {
	if cfg.GoveeKeySubjectPrefix != "" {
		deps.GoveeKeys = core.NewGoveeKeyClient(nc, cfg.GoveeKeySubjectPrefix)
	}
	if cfg.SpotifyKeySubjectPrefix != "" {
		deps.SpotifyKeys = core.NewSpotifyKeyClient(nc, cfg.SpotifyKeySubjectPrefix)
	}
	if cfg.FetchKeySubjectPrefix != "" {
		deps.FetchKeys = core.NewFetchKeyClient(nc, cfg.FetchKeySubjectPrefix)
	}
	if cfg.FetchProjectionSubject != "" {
		deps.FetchDefs = fetchDefSource{client: newFetchProjection(nc, valkeyClient, cfg.FetchProjectionSubject)}
	}
}

// newFetchProjection builds the read-side client for the commands service's
// fetch-definition projection: in-process cache fronting the shared Valkey
// hash, with the projector's tier-3 verb as the cold-read fallback. Read-only
// — gossip never writes definitions; ownership stays with commands.
func newFetchProjection(nc *nats.Conn, vc valkey_go.Client, subject string) *projection.Client {
	return projection.NewClient(projection.Config{
		Store: projection.NewStore(vc),
		NC:    nc,
		Subjects: projection.Subjects{
			Fetches: subject,
		},
		TTL: projectionCacheTTL,
	})
}

// projectionCacheTTL bounds the in-process definition cache. Definitions are
// authoring data that changes rarely and heals on rename/delete via the
// invalidation events commands publishes; two minutes keeps an edit visible
// quickly without making every chat burst a Valkey round trip.
const projectionCacheTTL = 2 * time.Minute

// fetchDefSource adapts the projection client's uint64-keyed view onto
// provider.DefSource's string-keyed seam. A non-numeric broadcaster id is a
// caller bug upstream of us: no definitions can exist for it, so it reads as
// a clean not-found rather than an error.
type fetchDefSource struct {
	client *projection.Client
}

func (s fetchDefSource) FetchDef(ctx context.Context, broadcasterID, name string) (gossiprpc.FetchDef, bool, error) {
	uid, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil || uid == 0 {
		return gossiprpc.FetchDef{}, false, nil
	}
	view, ok, err := s.client.FetchDefs(ctx, uid, name)
	if err != nil || !ok {
		return gossiprpc.FetchDef{}, false, err
	}
	return gossiprpc.FetchDef{
		Name:     name,
		URL:      view.URL,
		JSONPath: view.JSONPath,
		KeyLabel: view.KeyLabel,
		IsActive: view.IsActive,
	}, true, nil
}
