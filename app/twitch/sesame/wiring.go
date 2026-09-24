// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/config"
	"ItsBagelBot/app/twitch/sesame/internal/consumer"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type infra struct {
	nc  *nats.Conn
	pub bus.Publisher
	sub bus.Subscriber
	vc  valkey.Client
}

type wireCtx struct {
	ctx context.Context
	in  infra
	cfg *config.Config
	log *zap.Logger
}

func dialNATS(cfg *config.Config, log *zap.Logger) (*nats.Conn, bus.Publisher, bus.Subscriber) {
	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	sub, err := bus.NewSubscriber(cfg.NATSURL, cfg.ConsumerName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	return nc, pub, sub
}

type engineRuntime struct {
	proj     *projection.Client
	live     *engine.ValkeyLiveStore
	timers   *engine.ValkeyTimerStore
	guard    *automod.Gate
	loyalty  engine.LoyaltyStore
	tick     *engine.ValkeyLoyaltyClock
	stats    *engine.LoyaltyReporter
	raffle   *engine.ValkeyRaffleStore
	duel     *engine.ValkeyDuelStore
	seq      *engine.Sequencer
	emotes   *automod.EmoteFetcher
	chatters *engine.ValkeyChatters
}

func buildDeps(w wireCtx, rt engineRuntime) engine.Deps {
	in, cfg, log := w.in, w.cfg, w.log
	gossipRPC := engine.NewGossipRPC(in.nc, cfg.GossipRPCPrefix)
	return engine.Deps{
		TrialStore:    in.vc,
		Proj:          rt.proj,
		Live:          rt.live,
		Greet:         engine.NewValkeyGreetStore(in.vc, cfg.LiveTTL, log),
		Cooldown:      engine.NewValkeyCooldown(in.vc),
		Special:       engine.NewSpecialSet(cfg.SpecialUserIDs),
		Pub:           in.pub,
		Commands:      engine.NewCommandsRPC(in.nc, cfg.CommandsDashboardPrefix),
		Quotes:        engine.NewQuotesRPC(in.nc, cfg.ModulesRPCPrefix),
		Gossip:        gossipRPC,
		CustomFetch:   gossipRPC,
		Followage:     engine.NewFollowageRPC(in.nc, cfg.OutgressRPCPrefix),
		AccountAge:    engine.NewAccountAgeRPC(in.nc, cfg.OutgressRPCPrefix),
		Uptime:        engine.NewUptimeRPC(in.nc, cfg.OutgressRPCPrefix),
		StreamInfo:    engine.NewStreamInfoRPC(in.nc, cfg.OutgressRPCPrefix),
		ChannelCounts: engine.NewChannelCountsRPC(in.nc, cfg.OutgressRPCPrefix),
		Viewers:       engine.NewViewerRPC(in.nc, cfg.OutgressRPCPrefix, rt.chatters, log),
		Log:           log,
		Automod:       rt.guard,
		Reputation:    engine.NewValkeyReputation(in.vc, 6*time.Hour, log),
		Campaign:      engine.NewValkeyCampaign(in.vc, log),
		Queue:         engine.NewValkeyQueueStore(in.vc, 24*time.Hour, log),
		SongQueue:     engine.NewValkeySongQueueStore(in.vc, 24*time.Hour, log),
		Raffle:        rt.raffle,
		Duel:          rt.duel,
		Timers:        rt.timers,
		ChatLines:     rt.timers,

		Loyalty:     rt.loyalty,
		LoyaltyTick: rt.tick,
		Stats:       rt.stats,

		Personality: engine.NewValkeyPersonality(in.vc, engine.NewPersonalityRPC(in.nc, cfg.ModulesRPCPrefix), log),

		EmotePlay: engine.NewValkeyEmotePlay(in.vc),

		Emotes: engine.EmoteCatalogFrom(rt.emotes),

		Dedup: newDedup(w),

		Seq: rt.seq,

		PublicBaseURL: cfg.PublicBaseURL,
	}
}

const (
	sesameSeenPrefix       = "sesame:seen:"
	idempotencyLRUCapacity = 100_000
)

func newDedup(w wireCtx) *engine.EventDedup {
	if !w.cfg.IdempotencyEnabled {
		return nil
	}
	store := idempotency.NewTiered(idempotencyLRUCapacity,
		idempotency.NewValkeyStore(w.in.vc, sesameSeenPrefix, w.log))
	return engine.NewEventDedup(store, sesameSeenPrefix, w.cfg.IdempotencyTTL, w.log)
}

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

func newTimers(w wireCtx, proj *projection.Client, live *engine.ValkeyLiveStore) *engine.ValkeyTimerStore {
	return engine.NewValkeyTimerStore(w.in.vc, w.in.pub, proj, live, engine.TimersConfig{
		OutgressPremiumSubject:   w.cfg.OutgressPremiumSubject,
		OutgressStandardSubject:  w.cfg.OutgressStandardSubject,
		KeyspaceDB:               0,
		NC:                       w.in.nc,
		ModulesInvalidateSubject: w.cfg.CacheInvalidationPrefix + ".modules",
		Log:                      w.log,
	})
}

func startTimerWatchers(w wireCtx, timers *engine.ValkeyTimerStore) {
	go timers.StartExpiryWatcher(w.ctx)
	go timers.StartRearmWatcher(w.ctx)
	go timers.StartReconciler(w.ctx)
}

func newRaffle(w wireCtx, proj *projection.Client) *engine.ValkeyRaffleStore {
	raffle := engine.NewValkeyRaffleStore(w.in.vc, engine.RaffleConfig{
		OutgressPremiumSubject:  w.cfg.OutgressPremiumSubject,
		OutgressStandardSubject: w.cfg.OutgressStandardSubject,
		Pub:                     w.in.pub,
		Proj:                    proj,
	}, w.log)
	go raffle.StartExpiryWatcher(w.ctx)
	return raffle
}

func newDuel(w wireCtx, proj *projection.Client, loyalty engine.LoyaltyStore) *engine.ValkeyDuelStore {
	duel := engine.NewValkeyDuelStore(w.in.vc, engine.DuelConfig{
		OutgressPremiumSubject:  w.cfg.OutgressPremiumSubject,
		OutgressStandardSubject: w.cfg.OutgressStandardSubject,
		Pub:                     w.in.pub,
		Proj:                    proj,
		Wallet:                  engine.NewLoyaltyWallet(loyalty),
	}, w.log)
	go duel.StartExpiryWatcher(w.ctx)
	return duel
}

type loyaltyDeps struct {
	proj     *projection.Client
	live     *engine.ValkeyLiveStore
	reporter *engine.LoyaltyReporter
	chatters *engine.ValkeyChatters
}

func newLoyalty(w wireCtx, deps loyaltyDeps) (engine.LoyaltyStore, *engine.ValkeyLoyaltyClock) {
	store := engine.NewValkeyLoyaltyStore(w.in.vc, engine.NewLoyaltyRPC(w.in.nc, w.cfg.LoyaltyRPCPrefix), deps.reporter, w.log)
	tick := newLoyaltyClock(w, deps)
	return store, tick
}

func newLoyaltyClock(w wireCtx, deps loyaltyDeps) *engine.ValkeyLoyaltyClock {
	clock := engine.NewValkeyLoyaltyClock(w.in.vc, w.in.nc, deps.proj, deps.live, deps.reporter, engine.LoyaltyClockConfig{
		OutgressRPCPrefix:        w.cfg.OutgressRPCPrefix,
		ModulesInvalidateSubject: w.cfg.CacheInvalidationPrefix + ".modules",
		BotUserID:                w.cfg.BotUserID,
		KeyspaceDB:               0,
		Publisher:                w.in.pub,
		OutgressSystemSubject:    w.cfg.OutgressSystemSubject,
		ViewerSnapshots:          deps.chatters,
		Log:                      w.log,
	})
	go clock.StartExpiryWatcher(w.ctx)
	go clock.StartRearmWatcher(w.ctx)
	go clock.StartReconciler(w.ctx)
	return clock
}

func newPipeline(deps engine.Deps, registry *engine.Registry, cfg *config.Config) *engine.Pipeline {
	return engine.NewPipeline(deps, registry, engine.Config{
		BotID:             cfg.BotUserID,
		OutgressPremium:   cfg.OutgressPremiumSubject,
		OutgressStandard:  cfg.OutgressStandardSubject,
		CountUses:         true,
		AutomodEnforce:    cfg.AutomodEnforce,
		ShieldEnabled:     cfg.ShieldEnabled,
		AutoRefundChannel: cfg.AutoRefundChannel,
	})
}

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
