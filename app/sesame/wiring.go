package main

// This file is sesame's composition root: it dials the shared clients and
// assembles engine.Deps plus the per-broadcaster stores. A new module
// dependency is a field in engine.Deps (app/sesame/engine/deps.go) and a
// construction line here; main.go only sequences boot/shutdown and should not
// need to change.

import (
	"context"
	"time"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/internal/config"
	"ItsBagelBot/app/sesame/internal/consumer"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// infra bundles the process's shared clients so the wiring helpers take one
// value instead of a long argument list.
type infra struct {
	nc  *nats.Conn
	pub bus.Publisher
	sub bus.Subscriber
	vc  valkey.Client
}

// wireCtx carries the cross-cutting inputs every store constructor needs
// (lifecycle context, shared clients, config, logger), so the constructors
// below take it as one value plus their store-specific dependencies.
type wireCtx struct {
	ctx context.Context
	in  infra
	cfg *config.Config
	log *zap.Logger
}

// dialNATS opens the core RPC connection (projector fallback) and the JetStream
// publisher/subscriber that drive the lanes. Any failure is fatal.
func dialNATS(cfg *config.Config, log *zap.Logger) (*nats.Conn, bus.Publisher, bus.Subscriber) {
	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	// ConsumerName defaults to "worker" so sesame binds the worker's existing lane
	// consumer (drop-in on the same lanes; no DeliverAll replay, rollout overlap
	// load-balances instead of double-processing).
	sub, err := bus.NewSubscriber(cfg.NATSURL, cfg.ConsumerName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	return nc, pub, sub
}

// engineRuntime bundles the per-broadcaster stores main builds before assembling
// engine.Deps, so buildDeps takes one value instead of a long argument list.
type engineRuntime struct {
	proj    *projection.Client
	live    *engine.ValkeyLiveStore
	timers  *engine.ValkeyTimerStore
	guard   *automod.Gate
	loyalty engine.LoyaltyStore
	tick    *engine.ValkeyLoyaltyClock
}

// buildDeps assembles the engine.Deps every module fn captures. modules.All turns
// it into the built modules (core commands + bagel, live tracker, opt-in shoutout)
// the engine registry indexes; adding a feature is a new file in app/sesame/modules
// plus one line in all.go, no wiring here. The Valkey/RPC-backed stores are built
// from the process's shared clients (in) and the config.
func buildDeps(w wireCtx, rt engineRuntime) engine.Deps {
	in, cfg, log := w.in, w.cfg, w.log
	return engine.Deps{
		Proj:       rt.proj,
		Live:       rt.live,
		Greet:      engine.NewValkeyGreetStore(in.vc, cfg.LiveTTL, log),
		Cooldown:   engine.NewValkeyCooldown(in.vc),
		Special:    engine.NewSpecialSet(cfg.SpecialUserIDs),
		Pub:        in.pub,
		Commands:   engine.NewCommandsRPC(in.nc, cfg.CommandsDashboardPrefix),
		Quotes:     engine.NewQuotesRPC(in.nc, cfg.ModulesRPCPrefix),
		Gateway:    engine.NewGatewayRPC(in.nc, cfg.GatewayRPCPrefix),
		Followage:  engine.NewFollowageRPC(in.nc, cfg.OutgressRPCPrefix),
		AccountAge: engine.NewAccountAgeRPC(in.nc, cfg.OutgressRPCPrefix),
		Log:        log,
		Automod:    rt.guard,
		Reputation: engine.NewValkeyReputation(in.vc, 6*time.Hour, log),
		Campaign:   engine.NewValkeyCampaign(in.vc, log),
		Queue:      engine.NewValkeyQueueStore(in.vc, 24*time.Hour, log),
		Timers:     rt.timers,

		Loyalty:     rt.loyalty,
		LoyaltyTick: rt.tick,

		Personality: engine.NewValkeyPersonality(in.vc, engine.NewPersonalityRPC(in.nc, cfg.ModulesRPCPrefix), log),

		PublicBaseURL: cfg.PublicBaseURL,
	}
}

// newProjection builds the settings-projection reader (in-process cache fronting
// Valkey, with a projector RPC fallback) and starts its cache invalidation
// listener.
func newProjection(w wireCtx) *projection.Client {
	proj := projection.NewClient(projection.Config{
		Store: projection.NewStore(w.in.vc),
		NC:    w.in.nc,
		Subjects: projection.Subjects{
			Users:    w.cfg.ProjectionUsersSubject,
			Modules:  w.cfg.ProjectionModulesSubject,
			Commands: w.cfg.ProjectionCommandsSubject,
		},
		TTL: projectionCacheTTL,
		Log: w.log,
	})
	proj.StartInvalidationListener(w.cfg.CacheInvalidationPrefix)
	proj.StartOccupancyLogger(w.ctx, cacheOccupancyInterval)
	return proj
}

// newLive builds the Valkey-backed live store — a dedicated live:<id> key read
// through an in-process cache, written from the stream events sesame consumes,
// with a projector RPC fallback on a cold key and a key-expiry re-check against
// Twitch (via the outgress system lane) — and starts its listeners.
func newLive(w wireCtx) *engine.ValkeyLiveStore {
	live := engine.NewValkeyLiveStore(w.in.vc, w.in.nc, w.in.pub, engine.LiveConfig{
		TTL:                   w.cfg.LiveTTL,
		CacheTTL:              projectionCacheTTL,
		ProjectorLiveSubject:  w.cfg.ProjectionLiveSubject,
		OutgressSystemSubject: w.cfg.OutgressSystemSubject,
		CacheInvalidatePrefix: w.cfg.CacheInvalidationPrefix,
		KeyspaceDB:            0,
		Log:                   w.log,
	})
	live.StartInvalidationListener()
	go live.StartExpiryWatcher(w.ctx)
	return live
}

// newTimers builds the Valkey-backed timer store — one schedule key per
// enabled repeating message, armed on stream.online and fired off key expiry
// (see live_valkey.go's key-expiry idiom, which this shares the deployment's
// notify-keyspace-events config with) — and starts its expiry watcher plus the
// rearm watcher that arms a live broadcaster's timers mid-stream when a
// dashboard save changes their modules blob (so a timer added while already live
// starts this session, not next stream).
func newTimers(w wireCtx, proj *projection.Client, live *engine.ValkeyLiveStore) *engine.ValkeyTimerStore {
	timers := engine.NewValkeyTimerStore(w.in.vc, w.in.pub, proj, live, engine.TimersConfig{
		OutgressPremiumSubject:   w.cfg.OutgressPremiumSubject,
		OutgressStandardSubject:  w.cfg.OutgressStandardSubject,
		KeyspaceDB:               0,
		NC:                       w.in.nc,
		ModulesInvalidateSubject: w.cfg.CacheInvalidationPrefix + ".modules",
		Log:                      w.log,
	})
	go timers.StartExpiryWatcher(w.ctx)
	go timers.StartRearmWatcher(w.ctx)
	go timers.StartReconciler(w.ctx)
	return timers
}

// newLoyalty builds the loyalty store (a Valkey live view fronting the loyalty
// service) and its watch clock, both fed by the shared reporter that batches
// accruals/bumps onto data.loyalty.*.
func newLoyalty(w wireCtx, proj *projection.Client, live *engine.ValkeyLiveStore, reporter *engine.LoyaltyReporter) (engine.LoyaltyStore, *engine.ValkeyLoyaltyClock) {
	store := engine.NewValkeyLoyaltyStore(w.in.vc, engine.NewLoyaltyRPC(w.in.nc, w.cfg.LoyaltyRPCPrefix), reporter, w.log)
	tick := newLoyaltyClock(w, proj, live, reporter)
	return store, tick
}

// newLoyaltyClock builds the Valkey-backed watch tick — one schedule key per
// live broadcaster with an enabled loyalty module, armed on stream.online and
// fired off key expiry (the timers idiom) into a chatters fetch + accrual —
// and starts its expiry, rearm and reconciler watchers.
func newLoyaltyClock(w wireCtx, proj *projection.Client, live *engine.ValkeyLiveStore, reporter *engine.LoyaltyReporter) *engine.ValkeyLoyaltyClock {
	clock := engine.NewValkeyLoyaltyClock(w.in.vc, w.in.nc, proj, live, reporter, engine.LoyaltyClockConfig{
		OutgressRPCPrefix:        w.cfg.OutgressRPCPrefix,
		ModulesInvalidateSubject: w.cfg.CacheInvalidationPrefix + ".modules",
		BotUserID:                w.cfg.BotUserID,
		KeyspaceDB:               0,
		Log:                      w.log,
	})
	go clock.StartExpiryWatcher(w.ctx)
	go clock.StartRearmWatcher(w.ctx)
	go clock.StartReconciler(w.ctx)
	return clock
}

func newPipeline(deps engine.Deps, registry *engine.Registry, cfg *config.Config) *engine.Pipeline {
	return engine.NewPipeline(deps, registry, engine.Config{
		BotID:            cfg.BotUserID,
		OutgressPremium:  cfg.OutgressPremiumSubject,
		OutgressStandard: cfg.OutgressStandardSubject,
		CountUses:        true,
		AutomodEnforce:   cfg.AutomodEnforce,
		ShieldEnabled:    cfg.ShieldEnabled,
	})
}

// newConsumer builds the one autoscaling consumer that drains the premium and
// standard lanes into a shared pool, with premium reserving a slice so it is
// never starved. Live events ride these same lanes, so there is no separate
// stream consumer.
func newConsumer(sub bus.Subscriber, nrApp *newrelic.Application, cfg *config.Config, log *zap.Logger) *consumer.Consumer {
	return consumer.New(sub, nrApp, consumer.Config{
		Lanes: consumer.Lanes{PremiumSubject: cfg.PremiumSubject, StandardSubject: cfg.StandardSubject},
		Policy: bus.ScalePolicy{
			MinRoutines:    cfg.MinRoutines,
			MaxRoutines:    cfg.MaxRoutines,
			MinConsumers:   cfg.MinConsumers,
			MaxConsumers:   cfg.MaxConsumers,
			ScaleUpAfter:   cfg.ScaleUpAfter,
			ScaleDownAfter: cfg.ScaleDownAfter,
		},
		PremiumReserve: cfg.PremiumReserve,
	}, log)
}
