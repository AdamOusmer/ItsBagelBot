// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/internal/config"
	"ItsBagelBot/app/twitch/sesame/modules"
	"ItsBagelBot/internal/activity"
	"ItsBagelBot/internal/chatvolume"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"

	"go.uber.org/zap"
)

const (
	serviceName = "sesame"
	queueGroup  = "sesame-rpc"
)

const projectionCacheTTL = 30 * time.Second

const cacheOccupancyInterval = 5 * time.Minute

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx, nrApp := core.Log, core.Ctx, core.NR

	i18n.WarnGaps(log)

	cfg := config.Load()

	// internal/consumer subscribes TWITCH_INGRESS_RETRY under the same flag; change both together.
	owned := bus.IngressLaneSpecs()
	if bus.FlowConsumeEnabled() {
		owned = append(owned, bus.TwitchIngressRetryStream)
	}
	svcboot.FatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, owned, log),
		"failed to provision the TWITCH_INGRESS streams")

	nc, pub, sub := dialNATS(cfg, log)
	defer nc.Close()
	defer func() { _ = pub.Close() }()
	defer func() { _ = sub.Close() }()

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()

	activity.SetSink(activity.NewStore(valkeyClient))

	w := wireCtx{ctx: ctx, in: infra{nc: nc, pub: pub, sub: sub, vc: valkeyClient}, cfg: cfg, log: log}

	proj := newProjection(w)
	defer proj.Close()

	live := newLive(w)
	defer live.Close()

	timers := newTimers(w, proj, live)

	loyaltyReporter := engine.NewLoyaltyReporter(pub, log)
	defer loyaltyReporter.Close()
	chatters := engine.NewValkeyChatters(valkeyClient, log)
	loyalty, loyaltyTick := newLoyalty(w, loyaltyDeps{proj: proj, live: live, reporter: loyaltyReporter, chatters: chatters})

	raffle := newRaffle(w, proj)
	duel := newDuel(w, proj, loyalty)

	guard := automod.New()
	if cfg.AdaptiveEnabled {
		guard.SetExtraEmotes(automod.NewVocab())
		guard.SetBaseline(automod.NewBaseline(automod.DefaultCeiling()))
	}
	var emotes *automod.EmoteFetcher
	if cfg.EmotesEnabled {
		emotes = automod.NewEmoteFetcher(nil, automod.DefaultEmoteEndpoints)
	}
	deps := buildDeps(w, engineRuntime{
		proj: proj, live: live, timers: timers, guard: guard, loyalty: loyalty, tick: loyaltyTick,
		stats: loyaltyReporter, raffle: raffle, duel: duel, emotes: emotes,
		seq: engine.NewSequencer(), chatters: chatters,
	})
	registry := engine.NewRegistry(log, modules.All(deps)...)
	startRefreshers(w, guard, emotes)

	pipe := newPipeline(deps, registry, cfg)
	defer pipe.Close()

	timers.WirePipeline(pipe)
	startTimerWatchers(w, timers)

	pipe.RegisterObserver(activityObserver{})

	pipe.RegisterObserver(chatVolumeObserver{store: chatvolume.New(valkeyClient, log)})

	go engine.NewTrialPromotion(valkeyClient, engine.NewLoyaltyRPC(nc, cfg.LoyaltyRPCPrefix), log.Named("trial-promotion")).Run(ctx)

	weighted, err := newConsumer(sub, nrApp, cfg, log).Start(ctx, pipe.Process)
	svcboot.FatalIf(log, err, "failed to start consumer")
	serveHealth(w)
	logReady(cfg, deps.Special.Len(), log)

	<-ctx.Done()
	drainInflight(weighted, cfg.DrainTimeout, log)
}

// Do not wrap the probes in health.Degrades: a total ingress outage would never page.
func serveHealth(w wireCtx) {
	svcboot.ServeHealth(svcboot.Health{
		Log: w.log, NC: w.in.nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: w.cfg.ListenAddr,
	},
		bus.LaneCheck("ingress_events", w.in.sub),
		bus.HealthProbe(w.in.nc, "ingress"),
		bus.HealthProbe(w.in.nc, "outgress"),
	)
}

func startRefreshers(w wireCtx, guard *automod.Gate, emotes *automod.EmoteFetcher) {
	if emotes != nil {
		go refreshEmotes(w.ctx, emotes, guard, w.log)
	}
	if dir := env.Get("SESAME_AUTOMOD_LEXICON_DIR", ""); dir != "" {
		go reloadLexicon(w.ctx, dir, guard, w.log)
	}
	if w.cfg.LinkCheckEnabled {
		go runLinkCheck(w.ctx, guard, w.cfg, w.log)
	}
}

func logReady(cfg *config.Config, specialUsers int, log *zap.Logger) {
	log.Info("sesame ready",
		zap.String("consumer_name", cfg.ConsumerName),
		zap.String("premium_subject", cfg.PremiumSubject),
		zap.String("standard_subject", cfg.StandardSubject),
		zap.Bool("flow_consume", bus.FlowConsumeEnabled()),
		zap.Int("min_routines", cfg.MinRoutines),
		zap.Int("max_routines", cfg.MaxRoutines),
		zap.Int("min_consumers", cfg.MinConsumers),
		zap.Int("max_consumers", cfg.MaxConsumers),
		zap.Int("premium_reserve_percent", cfg.PremiumReserve),
		zap.Int("special_users", specialUsers),
		zap.Duration("live_ttl", cfg.LiveTTL),
	)
}

func drainInflight(weighted *bus.Weighted, timeout time.Duration, log *zap.Logger) {
	log.Info("sesame shutting down, draining in-flight events", zap.Duration("timeout", timeout))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := weighted.Drain(ctx); err != nil {
		log.Warn("drain deadline exceeded; in-flight events left for redelivery", zap.Error(err))
		return
	}
	log.Info("in-flight events drained")
}

const emoteRefreshInterval = time.Hour

const lexiconReloadInterval = 5 * time.Minute

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

func refreshEmotes(ctx context.Context, fetcher *automod.EmoteFetcher, guard *automod.Gate, log *zap.Logger) {
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
