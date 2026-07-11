package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/sesame/automod"
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

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/valkey-io/valkey-go"
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

	nc, pub, sub := dialNATS(cfg, log)
	defer nc.Close()
	defer func() { _ = pub.Close() }()
	defer func() { _ = sub.Close() }()

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	in := infra{nc: nc, pub: pub, sub: sub, vc: valkeyClient}

	proj := newProjection(in, cfg, log)
	defer proj.Close()

	live := newLive(ctx, in, cfg, log)
	defer live.Close()

	timers := newTimers(ctx, in, proj, live, cfg, log)

	// Loyalty: the reporter batches every accrual/bump into data.loyalty.*
	// events; the store fronts the loyalty service with a Valkey live view; the
	// clock drives the per-channel watch tick while live.
	loyaltyReporter := engine.NewLoyaltyReporter(pub, log)
	defer loyaltyReporter.Close() // flushes pending accruals on shutdown
	loyalty := engine.NewValkeyLoyaltyStore(valkeyClient, engine.NewLoyaltyRPC(nc, cfg.LoyaltyRPCPrefix), loyaltyReporter, log)
	loyaltyTick := newLoyaltyClock(ctx, in, proj, live, loyaltyReporter, cfg, log)

	// Deps is the bundle every module fn captures; main builds it once. modules.All
	// returns the built modules (core commands + bagel, live tracker, opt-in
	// shoutout), which the engine registry indexes. Adding a feature is a new file
	// in app/sesame/modules plus one line in all.go — no wiring here.
	// guard is the inline automod gate; hoisted so the emote refresher can install
	// its false-positive-suppression sets onto the same instance.
	guard := automod.New()

	deps := engine.Deps{
		Proj:       proj,
		Live:       live,
		Greet:      engine.NewValkeyGreetStore(valkeyClient, cfg.LiveTTL, log),
		Cooldown:   engine.NewValkeyCooldown(valkeyClient),
		Dedup:      engine.NewValkeyDedup(valkeyClient, 10*time.Minute),
		Special:    engine.NewSpecialSet(cfg.SpecialUserIDs),
		Pub:        pub,
		Commands:   engine.NewCommandsRPC(nc, cfg.CommandsDashboardPrefix),
		Quotes:     engine.NewQuotesRPC(nc, cfg.ModulesRPCPrefix),
		Gateway:    engine.NewGatewayRPC(nc, cfg.GatewayRPCPrefix),
		Followage:  engine.NewFollowageRPC(nc, cfg.OutgressRPCPrefix),
		Log:        log,
		Automod:    guard,
		Reputation: engine.NewValkeyReputation(valkeyClient, 6*time.Hour, log),
		Campaign:   engine.NewValkeyCampaign(valkeyClient, log),
		Queue:      engine.NewValkeyQueueStore(valkeyClient, 24*time.Hour, log),
		Timers:     timers,

		Loyalty:     loyalty,
		LoyaltyTick: loyaltyTick,

		PublicBaseURL: cfg.PublicBaseURL,
	}
	registry := engine.NewRegistry(log, modules.All(deps)...)

	if cfg.EmotesEnabled {
		go refreshEmotes(ctx, guard, log)
	}
	if dir := env.Get("SESAME_AUTOMOD_LEXICON_DIR", ""); dir != "" {
		go reloadLexicon(ctx, dir, guard, log)
	}

	pipe := newPipeline(deps, registry, cfg)
	defer pipe.Close() // flushes pending use-counter ticks on shutdown

	if err := newConsumer(sub, nrApp, cfg, log).Start(ctx, pipe.Process); err != nil {
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
		zap.Int("special_users", deps.Special.Len()),
		zap.Duration("live_ttl", cfg.LiveTTL),
	)

	<-ctx.Done()

	log.Info("sesame shutting down")
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

// emoteRefreshInterval is how often the global third-party emote sets are
// re-fetched. They change slowly; hourly keeps the caps false-positive suppression
// fresh at negligible cost (a few small unauthenticated GETs).
const emoteRefreshInterval = time.Hour

// lexiconReloadInterval is how often the lexicon override directory is re-read.
// The pattern artifact is a mounted ConfigMap (the Flux-managed reviewable-list
// pattern); a few minutes of staleness on a word-list change is fine.
const lexiconReloadInterval = 5 * time.Minute

// reloadLexicon loads the lexicon override directory at startup and re-reads it
// on a slow ticker, swapping the compiled set into the gate. A load failure is
// logged and the previous (or embedded) lexicon stays active, so a bad mount can
// never blank the floor lists.
func reloadLexicon(ctx context.Context, dir string, guard *automod.Gate, log *zap.Logger) {
	load := func() {
		l, err := automod.LoadLexiconDir(dir)
		if err != nil {
			log.Warn("lexicon override load failed, keeping previous", zap.String("dir", dir), zap.Error(err))
			return
		}
		guard.SetLexicon(l)
		log.Info("lexicon override loaded", zap.String("dir", dir))
	}

	load()
	ticker := time.NewTicker(lexiconReloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			load()
		}
	}
}

// refreshEmotes keeps the automod's third-party emote set current: it installs the
// global BTTV/FFZ/7TV codes once at startup, then re-fetches on a slow ticker. A
// fetch failure is logged and the previous set is kept; it never blocks the gate,
// which treats an absent set as "suppress nothing" (the pre-emote behavior).
func refreshEmotes(ctx context.Context, guard *automod.Gate, log *zap.Logger) {
	fetcher := automod.NewEmoteFetcher(nil, automod.DefaultEmoteEndpoints)

	load := func() {
		n, err := fetcher.Refresh(ctx, guard)
		if err != nil {
			log.Warn("emote set refresh partial or failed", zap.Int("codes", n), zap.Error(err))
			return
		}
		log.Info("emote set refreshed", zap.Int("codes", n))
	}

	load()
	ticker := time.NewTicker(emoteRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			load()
		}
	}
}

// infra bundles the process's shared clients so the wiring helpers take one
// value instead of a long argument list.
type infra struct {
	nc  *nats.Conn
	pub message.Publisher
	sub message.Subscriber
	vc  valkey.Client
}

// dialNATS opens the core RPC connection (projector fallback) and the JetStream
// publisher/subscriber that drive the lanes. Any failure is fatal.
func dialNATS(cfg *config.Config, log *zap.Logger) (*nats.Conn, message.Publisher, message.Subscriber) {
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

// newProjection builds the settings-projection reader (in-process cache fronting
// Valkey, with a projector RPC fallback) and starts its cache invalidation
// listener.
func newProjection(in infra, cfg *config.Config, log *zap.Logger) *projection.Client {
	proj := projection.NewClient(projection.Config{
		Store: projection.NewStore(in.vc),
		NC:    in.nc,
		Subjects: projection.Subjects{
			Users:    cfg.ProjectionUsersSubject,
			Modules:  cfg.ProjectionModulesSubject,
			Commands: cfg.ProjectionCommandsSubject,
		},
		TTL: projectionCacheTTL,
		Log: log,
	})
	proj.StartInvalidationListener(cfg.CacheInvalidationPrefix)
	return proj
}

// newLive builds the Valkey-backed live store — a dedicated live:<id> key read
// through an in-process cache, written from the stream events sesame consumes,
// with a projector RPC fallback on a cold key and a key-expiry re-check against
// Twitch (via the outgress system lane) — and starts its listeners.
func newLive(ctx context.Context, in infra, cfg *config.Config, log *zap.Logger) *engine.ValkeyLiveStore {
	live := engine.NewValkeyLiveStore(in.vc, in.nc, in.pub, engine.LiveConfig{
		TTL:                   cfg.LiveTTL,
		CacheTTL:              projectionCacheTTL,
		ProjectorLiveSubject:  cfg.ProjectionLiveSubject,
		OutgressSystemSubject: cfg.OutgressSystemSubject,
		CacheInvalidatePrefix: cfg.CacheInvalidationPrefix,
		KeyspaceDB:            0,
		Log:                   log,
	})
	live.StartInvalidationListener()
	go live.StartExpiryWatcher(ctx)
	return live
}

// newTimers builds the Valkey-backed timer store — one schedule key per
// enabled repeating message, armed on stream.online and fired off key expiry
// (see live_valkey.go's key-expiry idiom, which this shares the deployment's
// notify-keyspace-events config with) — and starts its expiry watcher plus the
// rearm watcher that arms a live broadcaster's timers mid-stream when a
// dashboard save changes their modules blob (so a timer added while already live
// starts this session, not next stream).
func newTimers(ctx context.Context, in infra, proj *projection.Client, live *engine.ValkeyLiveStore, cfg *config.Config, log *zap.Logger) *engine.ValkeyTimerStore {
	timers := engine.NewValkeyTimerStore(in.vc, in.pub, proj, live, engine.TimersConfig{
		OutgressPremiumSubject:   cfg.OutgressPremiumSubject,
		OutgressStandardSubject:  cfg.OutgressStandardSubject,
		KeyspaceDB:               0,
		NC:                       in.nc,
		ModulesInvalidateSubject: cfg.CacheInvalidationPrefix + ".modules",
		Log:                      log,
	})
	go timers.StartExpiryWatcher(ctx)
	go timers.StartRearmWatcher(ctx)
	go timers.StartReconciler(ctx)
	return timers
}

// newLoyaltyClock builds the Valkey-backed watch tick — one schedule key per
// live broadcaster with an enabled loyalty module, armed on stream.online and
// fired off key expiry (the timers idiom) into a chatters fetch + accrual —
// and starts its expiry, rearm and reconciler watchers.
func newLoyaltyClock(ctx context.Context, in infra, proj *projection.Client, live *engine.ValkeyLiveStore, reporter *engine.LoyaltyReporter, cfg *config.Config, log *zap.Logger) *engine.ValkeyLoyaltyClock {
	clock := engine.NewValkeyLoyaltyClock(in.vc, in.nc, proj, live, reporter, engine.LoyaltyClockConfig{
		OutgressRPCPrefix:        cfg.OutgressRPCPrefix,
		ModulesInvalidateSubject: cfg.CacheInvalidationPrefix + ".modules",
		BotUserID:                cfg.BotUserID,
		KeyspaceDB:               0,
		Log:                      log,
	})
	go clock.StartExpiryWatcher(ctx)
	go clock.StartRearmWatcher(ctx)
	go clock.StartReconciler(ctx)
	return clock
}

// newConsumer builds the one autoscaling consumer that drains the premium and
// standard lanes into a shared pool, with premium reserving a slice so it is
// never starved. Live events ride these same lanes, so there is no separate
// stream consumer.
func newConsumer(sub message.Subscriber, nrApp *newrelic.Application, cfg *config.Config, log *zap.Logger) *consumer.Consumer {
	return consumer.New(sub, nrApp, consumer.Config{
		Lanes: consumer.Lanes{PremiumSubject: cfg.PremiumSubject, StandardSubject: cfg.StandardSubject},
		Policy: bus.ScalePolicy{
			MinRoutines:    cfg.MinRoutines,
			MaxRoutines:    cfg.MaxRoutines,
			MaxConsumers:   cfg.MaxConsumers,
			ScaleUpAfter:   cfg.ScaleUpAfter,
			ScaleDownAfter: cfg.ScaleDownAfter,
		},
		PremiumReserve: cfg.PremiumReserve,
	}, log)
}
