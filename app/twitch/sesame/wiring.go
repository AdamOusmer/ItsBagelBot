// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/internal/config"
	"ItsBagelBot/app/twitch/sesame/internal/consumer"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	pkgvalkey "ItsBagelBot/pkg/valkey"

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
	invalidations *bus.Invalidations
	ctx           context.Context
	in            infra
	cfg           *config.Config
	log           *zap.Logger
	expiry        *pkgvalkey.ExpiryHub
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
	proj      *projection.Client
	live      *engine.ValkeyLiveStore
	timers    *engine.ValkeyTimerStore
	guard     *automod.Gate
	loyalty   *engine.ValkeyLoyaltyStore
	tick      *engine.ValkeyLoyaltyClock
	stats     *engine.LoyaltyReporter
	raffle    *engine.ValkeyRaffleStore
	duel      *engine.ValkeyDuelStore
	seq       *engine.Sequencer
	emotes    *automod.EmoteFetcher
	chatters  *engine.ValkeyChatters
	runs      *engine.ValkeyRunLog
	analytics *analytics.Recorder
}

func buildDeps(w wireCtx, rt engineRuntime) engine.Deps {
	in, cfg, log := w.in, w.cfg, w.log
	gossipRPC := engine.NewGossipRPC(in.nc, cfg.GossipRPCPrefix)
	gossipRPC.SetAnalytics(rt.analytics)
	accountsRPC := engine.NewAccountAgeRPC(in.nc, cfg.OutgressRPCPrefix)
	followage := engine.NewFollowageRPC(in.nc, cfg.OutgressRPCPrefix)
	winners := engine.NewGiveawayWinnersRPC(in.nc, cfg.ModulesRPCPrefix)
	checker := engine.NewEligibility(rt.loyalty, followage, accountsRPC, winners)
	checker.WatchFreezes(w.invalidations)
	recipeState := engine.NewRecipeStateRPC(in.nc, cfg.ModulesRPCPrefix)
	return engine.Deps{
		AutomationHook: &engine.AutomationHook{},
		Analytics:      rt.analytics,
		Runtime:        newRuntime(w),
		Automations: engine.AutomationDeps{
			Source:        rt.proj,
			Runs:          rt.runs,
			Variables:     recipeState,
			Queue:         engine.NewValkeyRunQueue(in.vc),
			Durable:       engine.NewValkeyDurableRuns(in.vc),
			LongCooldowns: engine.NewMarkedLongCooldowns(in.vc, recipeState, log),
			Countdowns:    recipeState,
		},
		TrialStore:     in.vc,
		Proj:           rt.proj,
		Live:           rt.live,
		RewardResets:   engine.NewValkeyRewardResets(in.vc),
		Greet:          engine.NewValkeyGreetStore(in.vc, cfg.LiveTTL, log),
		Cooldown:       engine.NewValkeyCooldown(in.vc),
		Special:        engine.NewSpecialSet(cfg.SpecialUserIDs),
		Pub:            in.pub,
		Commands:       engine.NewCommandsRPC(in.nc, cfg.CommandsDashboardPrefix),
		Quotes:         engine.NewQuotesRPC(in.nc, cfg.ModulesRPCPrefix),
		Gossip:         gossipRPC,
		CustomFetch:    gossipRPC,
		Followage:      followage,
		AccountAge:     accountsRPC,
		TwitchAccounts: accountsRPC,
		Uptime:         engine.NewUptimeRPC(in.nc, cfg.OutgressRPCPrefix),
		StreamInfo:     engine.NewStreamInfoRPC(in.nc, cfg.OutgressRPCPrefix),
		ChannelCounts:  engine.NewChannelCountsRPC(in.nc, cfg.OutgressRPCPrefix),
		Viewers:        engine.NewViewerRPC(in.nc, cfg.OutgressRPCPrefix, rt.chatters, log),
		Log:            log,
		Automod:        rt.guard,
		Reputation:     engine.NewValkeyReputation(in.vc, 6*time.Hour, log),
		Campaign:       engine.NewValkeyCampaign(in.vc, log),
		Queue:          engine.NewValkeyQueueStore(in.vc, 24*time.Hour, log),
		SongQueue:      engine.NewValkeySongQueueStore(in.vc, 24*time.Hour, log),
		Raffle:         rt.raffle,
		Draw:           newDraw(w, rt, drawPorts{verifier: checker, winners: winners}),
		Eligibility:    checker,
		Duel:           rt.duel,
		Timers:         rt.timers,
		ChatLines:      rt.timers,

		Loyalty:       rt.loyalty,
		LoyaltyFreeze: rt.loyalty,
		LoyaltyTick:   rt.tick,
		Stats:         rt.stats,

		Personality: engine.NewValkeyPersonality(in.vc, engine.NewPersonalityRPC(in.nc, cfg.ModulesRPCPrefix), log),

		EmotePlay: engine.NewValkeyEmotePlay(in.vc),

		Emotes: engine.EmoteCatalogFrom(rt.emotes),

		Dedup: newDedup(w),

		Seq: rt.seq,

		PublicBaseURL: cfg.PublicBaseURL,

		LeaderboardBaseURL: cfg.LeaderboardBaseURL,
		QueueBaseURL:       cfg.QueueBaseURL,

		GiveawayWinners: winners,
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
		Store:         projection.NewStore(w.in.vc),
		NC:            w.in.nc,
		Invalidations: w.invalidations,
		Subjects: projection.Subjects{
			Users:    w.cfg.ProjectionUsersSubject,
			Modules:  w.cfg.ProjectionModulesSubject,
			Commands: w.cfg.ProjectionCommandsSubject,
		},
		TTL: projectionCacheTTL,
		Log: w.log,
	})
	proj.StartOccupancyLogger(w.ctx, cacheOccupancyInterval)
	return proj
}

func newLive(w wireCtx) *engine.ValkeyLiveStore {
	live := engine.NewValkeyLiveStore(w.in.vc, w.in.nc, w.in.pub, engine.LiveConfig{
		Runtime:               newRuntime(w),
		TTL:                   w.cfg.LiveTTL,
		CacheTTL:              projectionCacheTTL,
		ProjectorLiveSubject:  w.cfg.ProjectionLiveSubject,
		OutgressSystemSubject: w.cfg.OutgressSystemSubject,
		Invalidations:         w.invalidations,
		KeyspaceDB:            0,
		Expiry:                w.expiry,
		Log:                   w.log,
	})
	go live.StartExpiryWatcher(w.ctx)
	return live
}

func newRuntime(w wireCtx) *channelruntime.Client {
	return channelruntime.NewClient(projection.NewStore(w.in.vc), w.in.nc, w.cfg.ProjectionUsersSubject)
}

func newTimers(w wireCtx, proj *projection.Client, live *engine.ValkeyLiveStore) *engine.ValkeyTimerStore {
	return engine.NewValkeyTimerStore(w.in.vc, w.in.pub, proj, engine.TimersConfig{
		Live:                    live,
		Runtime:                 newRuntime(w),
		OutgressPremiumSubject:  w.cfg.OutgressPremiumSubject,
		OutgressStandardSubject: w.cfg.OutgressStandardSubject,
		KeyspaceDB:              0,
		Expiry:                  w.expiry,
		Invalidations:           w.invalidations,
		Log:                     w.log,
	})
}

func startTimerWatchers(w wireCtx, timers *engine.ValkeyTimerStore) {
	go timers.StartExpiryWatcher(w.ctx)
	timers.StartRearmWatcher(w.ctx)
	go timers.StartReconciler(w.ctx)
}

func announcementConfig(w wireCtx, proj projection.Reader) engine.AnnouncementConfig {
	return engine.AnnouncementConfig{
		Runtime:                 newRuntime(w),
		OutgressPremiumSubject:  w.cfg.OutgressPremiumSubject,
		OutgressStandardSubject: w.cfg.OutgressStandardSubject,
		Pub:                     w.in.pub,
		Proj:                    proj,
	}
}

func watchExpiry[T interface{ StartExpiryWatcher(context.Context) }](w wireCtx, store T) T {
	go store.StartExpiryWatcher(w.ctx)
	return store
}

type drawPorts struct {
	verifier draw.Verifier
	winners  engine.GiveawayWinnerRecorder
}

func newDraw(w wireCtx, rt engineRuntime, ports drawPorts) *draw.Store {
	log := w.log.Named("draw")
	announce := announcementConfig(w, rt.proj)
	store := draw.NewStore(w.in.vc, draw.Config{Log: log, LegacyKey: engine.RaffleStateKey,
		LegacyDeadlineKey: engine.RaffleDeadlineKey, Payer: engine.NewDrawPayer(rt.loyalty, announce.Runtime),
		Announcer: engine.NewDrawAnnouncer(announce, log, rt.analytics), Verifier: ports.verifier,
		Winners: engine.GiveawayWinnerSink{Winners: ports.winners}, Phases: engine.GiveawayPhaseObserver{Freeze: rt.loyalty, Log: log}})
	go store.Run(w.ctx)
	return store
}

func newRaffle(w wireCtx, proj *projection.Client, loyalty engine.LoyaltyStore, rec *analytics.Recorder) *engine.ValkeyRaffleStore {
	cfg := engine.RaffleConfig{AnnouncementConfig: announcementConfig(w, proj), Expiry: w.expiry, Wallet: engine.NewRaffleWallet(loyalty)}
	store := engine.NewValkeyRaffleStore(w.in.vc, cfg, w.log)
	store.SetAnalytics(rec)
	return watchExpiry(w, store)
}

func newDuel(w wireCtx, proj *projection.Client, loyalty engine.LoyaltyStore, rec *analytics.Recorder) *engine.ValkeyDuelStore {
	wallet := engine.NewLoyaltyWallet(loyalty)
	wallet.SetAnalytics(rec)
	cfg := engine.DuelConfig{AnnouncementConfig: announcementConfig(w, proj), Wallet: wallet, Expiry: w.expiry}
	store := engine.NewValkeyDuelStore(w.in.vc, cfg, w.log)
	store.SetAnalytics(rec)
	return watchExpiry(w, store)
}

func startTrialPromotion(w wireCtx, rec *analytics.Recorder) {
	promotion := engine.NewTrialPromotion(w.in.vc, engine.NewLoyaltyRPC(w.in.nc, engine.LoyaltyRPCPrefix(w.cfg.LoyaltyRPCPrefix)), w.log.Named("trial-promotion"))
	promotion.SetAnalytics(rec)
	go promotion.Run(w.ctx)
}

type loyaltyDeps struct {
	proj     *projection.Client
	live     *engine.ValkeyLiveStore
	reporter *engine.LoyaltyReporter
	chatters *engine.ValkeyChatters
}

func newLoyalty(w wireCtx, deps loyaltyDeps) (*engine.ValkeyLoyaltyStore, *engine.ValkeyLoyaltyClock) {
	store := engine.NewValkeyLoyaltyStore(w.in.vc, engine.NewLoyaltyRPC(w.in.nc, engine.LoyaltyRPCPrefix(w.cfg.LoyaltyRPCPrefix)), deps.reporter, w.log)
	store.BindInvalidations(w.invalidations)
	tick := newLoyaltyClock(w, deps, store)
	return store, tick
}

func newLoyaltyClock(w wireCtx, deps loyaltyDeps, store *engine.ValkeyLoyaltyStore) *engine.ValkeyLoyaltyClock {
	clock := engine.NewValkeyLoyaltyClock(w.in.vc, w.in.nc, engine.LoyaltyClockConfig{Proj: deps.proj, Live: deps.live, Reporter: deps.reporter,
		OutgressRPCPrefix:     w.cfg.OutgressRPCPrefix,
		Invalidations:         w.invalidations,
		BotUserID:             w.cfg.BotUserID,
		KeyspaceDB:            0,
		Expiry:                w.expiry,
		Publisher:             w.in.pub,
		OutgressSystemSubject: w.cfg.OutgressSystemSubject,
		ViewerSnapshots:       deps.chatters,
		WatchStatus:           store.WatchStatus,
		Log:                   w.log,
	})
	go clock.StartExpiryWatcher(w.ctx)
	clock.StartRearmWatcher(w.ctx)
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

func registerAutomationRPC(w wireCtx, pipe *engine.Pipeline, app *newrelic.Application) error {
	return engine.SubscribeAutomationRPC(w.in.nc, queueGroup, engine.AutomationRPCDeps{
		Pipeline: pipe,
		Client:   w.in.vc,
		App:      app,
		Log:      w.log,
	})
}

func registerGiveawayRPC(w wireCtx, deps engine.Deps, standings engine.GiveawayBalanceCounter, app *newrelic.Application) error {
	giveaway := engine.GiveawayRPC{Deps: deps, Announce: announcementConfig(w, deps.Proj), Standings: standings}
	return giveaway.Subscribe(bus.RPCWiring{NC: w.in.nc, Queue: queueGroup, App: app, Log: w.log})
}
