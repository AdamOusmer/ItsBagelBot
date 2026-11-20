// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/engine"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers"
	"ItsBagelBot/internal/domain/invalidate"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const serviceName = "gossip"

const queueGroup = "gossip-rpc"

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	cfg := config.Load()

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()

	nc := svcboot.MustRPCConn(core, cfg.NATSRPCURL)
	defer nc.Close()

	h := bus.NewInvalidations(nc, conf.CacheInvalidationPrefix.Get(), log)
	deps := buildDeps(gossipWiring{cfg: cfg, nc: nc, vc: valkeyClient, invalidations: h, log: log})
	watchSpotify(core.Ctx, deps, h)

	active := providers.All(cfg, deps)
	if len(active) == 0 {
		log.Warn("no providers configured; gossip will answer nothing")
	}
	wiring := bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log}
	svcboot.FatalIf(log, engine.Serve(wiring, cfg.SubjectPrefix, active),
		"failed to subscribe provider endpoints")

	svcboot.ServeHealth(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: cfg.ListenAddr,
	}, warpCheck())

	logReady(active, cfg, log)

	core.Await()
}

type gossipWiring struct {
	cfg           *config.Config
	nc            *nats.Conn
	vc            valkey_go.Client
	invalidations *bus.Invalidations
	log           *zap.Logger
}

func buildDeps(w gossipWiring) provider.Deps {
	deps := provider.Deps{
		Cache:   core.NewCache(core.NewValkeyStore(w.vc)),
		Limiter: ratelimit.New(w.vc),
		Log:     w.log,
	}
	wireKeyResolvers(w, &deps)
	return deps
}

func watchSpotify(ctx context.Context, deps provider.Deps, h *bus.Invalidations) {
	h.Watch(invalidate.ScopeSpotify, func(e cache.Event) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		key := core.Key("spotify", "token", strconv.FormatUint(e.ID, 10))
		if err := deps.Cache.DelJSON(ctx, key); err != nil {
			deps.Logger().Warn("spotify token cache invalidation failed", zap.Error(err))
		}
	})
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

// Degrade, never fail readiness: evicting the pod takes every other provider down with WARP.
func warpCheck() health.Check {
	return health.Degrades(health.Check{Name: "warp", Probe: core.WARPReachable})
}

func wireKeyResolvers(w gossipWiring, deps *provider.Deps) {
	cfg, nc := w.cfg, w.nc
	if cfg.GoveeKeySubjectPrefix != "" {
		deps.GoveeKeys = core.NewGoveeKeyClient(nc, cfg.GoveeKeySubjectPrefix)
	}
	if cfg.RiotLinkSubjectPrefix != "" {
		deps.RiotLinks = core.NewRiotLinkClient(nc, cfg.RiotLinkSubjectPrefix)
	}
	if cfg.SpotifyKeySubjectPrefix != "" {
		deps.SpotifyKeys = core.NewSpotifyKeyClient(nc, cfg.SpotifyKeySubjectPrefix)
	}
	if cfg.FetchKeySubjectPrefix != "" {
		deps.FetchKeys = core.NewFetchKeyClient(nc, cfg.FetchKeySubjectPrefix)
	}
	if cfg.FetchProjectionSubject != "" {
		deps.FetchDefs = fetchDefSource{client: newFetchProjection(w, cfg.FetchProjectionSubject)}
	}
}

func newFetchProjection(w gossipWiring, subject string) *projection.Client {
	return projection.NewClient(projection.Config{
		Store:         projection.NewStore(w.vc),
		NC:            w.nc,
		Invalidations: w.invalidations,
		Log:           w.log,
		Subjects: projection.Subjects{
			Fetches: subject,
		},
		TTL: projectionCacheTTL,
	})
}

const projectionCacheTTL = 2 * time.Minute

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
