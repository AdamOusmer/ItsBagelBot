package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/internal/config"
	"ItsBagelBot/app/sesame/internal/consumer"
	"ItsBagelBot/app/sesame/modules"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const serviceName = "sesame"

// projectionCacheTTL bounds how long a stale module/command/user view can linger
// in sesame before the next read re-checks Valkey and the projector.
const projectionCacheTTL = 30 * time.Second

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

	if err := bus.EnsureStreams(ctx, cfg.NATSURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	// Core connection drives the projector RPC fallback; JetStream pub/sub drives
	// the lanes.
	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	// ConsumerName defaults to "worker" so sesame binds the worker's existing lane
	// consumer (drop-in on the same lanes; no DeliverAll replay, rollout overlap
	// load-balances instead of double-processing).
	sub, err := bus.NewSubscriber(cfg.NATSURL, cfg.ConsumerName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	defer func() { _ = sub.Close() }()

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()
	valkeyStore := projection.NewStore(valkeyClient)

	proj := projection.NewClient(valkeyStore, nc, projection.Subjects{
		Users:    cfg.ProjectionUsersSubject,
		Modules:  cfg.ProjectionModulesSubject,
		Commands: cfg.ProjectionCommandsSubject,
	}, projectionCacheTTL, log)
	defer proj.Close()
	proj.StartInvalidationListener(cfg.CacheInvalidationPrefix)

	// Live status: a dedicated Valkey key (live:<id>) read through an in-process
	// cache, written from the stream events sesame already consumes, with a
	// projector RPC fallback on a cold key and a key-expiry re-check against Twitch
	// (via the outgress system lane).
	live := engine.NewValkeyLiveStore(valkeyClient, nc, pub, engine.LiveConfig{
		TTL:                   cfg.LiveTTL,
		CacheTTL:              projectionCacheTTL,
		ProjectorLiveSubject:  cfg.ProjectionLiveSubject,
		OutgressSystemSubject: cfg.OutgressSystemSubject,
		CacheInvalidatePrefix: cfg.CacheInvalidationPrefix,
		KeyspaceDB:            0,
	}, log)
	defer live.Close()
	live.StartInvalidationListener()
	go live.StartExpiryWatcher(ctx)

	greet := engine.NewValkeyGreetStore(valkeyClient, cfg.LiveTTL, log)
	cooldown := engine.NewValkeyCooldown(valkeyClient)
	special := engine.NewSpecialSet(cfg.SpecialUserIDs)

	// Deps is the bundle every module fn captures; main builds it once. modules.All
	// returns the built modules (core commands + bagel, live tracker, opt-in
	// shoutout), which the engine registry indexes. Adding a feature is a new file
	// in app/sesame/modules plus one line in all.go — no wiring here.
	deps := engine.Deps{
		Proj:     proj,
		Live:     live,
		Greet:    greet,
		Cooldown: cooldown,
		Special:  special,
		Pub:      pub,
		Log:      log,
	}
	registry := engine.NewRegistry(log, modules.All(deps)...)

	pipe := engine.NewPipeline(deps, registry, engine.Config{
		BotID:            cfg.BotUserID,
		OutgressPremium:  cfg.OutgressPremiumSubject,
		OutgressStandard: cfg.OutgressStandardSubject,
		CountUses:        true,
	})
	defer pipe.Close() // flushes pending use-counter ticks on shutdown

	// One autoscaling consumer drains the premium and standard lanes into a shared
	// pool of pipeline routines, with premium reserving a slice so it is never
	// starved. Live events ride these same lanes, so there is no separate stream
	// consumer.
	cons := consumer.New(sub, nrApp,
		consumer.Lanes{PremiumSubject: cfg.PremiumSubject, StandardSubject: cfg.StandardSubject},
		bus.ScalePolicy{
			MinRoutines:    cfg.MinRoutines,
			MaxRoutines:    cfg.MaxRoutines,
			MaxConsumers:   cfg.MaxConsumers,
			ScaleUpAfter:   cfg.ScaleUpAfter,
			ScaleDownAfter: cfg.ScaleDownAfter,
		},
		cfg.PremiumReserve,
		log,
	)
	if err := cons.Start(ctx, pipe.Process); err != nil {
		log.Fatal("failed to start consumer", zap.Error(err))
	}

	health.Serve(cfg.ListenAddr, nc.IsConnected)

	log.Info("sesame ready",
		zap.String("consumer_name", cfg.ConsumerName),
		zap.String("premium_subject", cfg.PremiumSubject),
		zap.String("standard_subject", cfg.StandardSubject),
		zap.Int("min_routines", cfg.MinRoutines),
		zap.Int("max_routines", cfg.MaxRoutines),
		zap.Int("max_consumers", cfg.MaxConsumers),
		zap.Int("premium_reserve_percent", cfg.PremiumReserve),
		zap.Int("special_users", special.Len()),
		zap.Duration("live_ttl", cfg.LiveTTL),
	)

	<-ctx.Done()

	log.Info("sesame shutting down")
}
