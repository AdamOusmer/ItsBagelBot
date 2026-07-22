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
	"ItsBagelBot/app/sesame/modules"
	"ItsBagelBot/internal/domain/i18n"
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

// cacheOccupancyInterval is how often sesame logs how full its projection caches
// run, so their capacities can be tuned to the observed working set. Slow enough
// to be noise-free at info level (one line per pod per interval).
const cacheOccupancyInterval = 5 * time.Minute

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
	log = monitor.WrapLogger(log, nrApp)
	defer monitor.Shutdown(nrApp)

	warnLocaleGaps(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	// Sesame owns TWITCH_INGRESS stream reconciliation. Other ingress consumers
	// receive consumer-only ACLs and twitch-ingress itself is publish-only.
	if err := bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.TwitchIngressStream}, log); err != nil {
		log.Fatal("failed to provision TWITCH_INGRESS stream", zap.Error(err))
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

	w := wireCtx{ctx: ctx, in: infra{nc: nc, pub: pub, sub: sub, vc: valkeyClient}, cfg: cfg, log: log}

	proj := newProjection(w)
	defer proj.Close()

	live := newLive(w)
	defer live.Close()

	timers := newTimers(w, proj, live)

	loyaltyReporter := engine.NewLoyaltyReporter(pub, log)
	defer loyaltyReporter.Close() // flushes pending accruals on shutdown
	loyalty, loyaltyTick := newLoyalty(w, proj, live, loyaltyReporter)

	// guard is the inline automod gate; hoisted so the emote/lexicon refreshers can
	// install their false-positive-suppression sets onto the same instance.
	guard := automod.New()
	deps := buildDeps(w, engineRuntime{
		proj: proj, live: live, timers: timers, guard: guard, loyalty: loyalty, tick: loyaltyTick,
	})
	registry := engine.NewRegistry(log, modules.All(deps)...)
	startRefreshers(ctx, guard, cfg, log)

	pipe := newPipeline(deps, registry, cfg)
	defer pipe.Close() // flushes pending use-counter ticks on shutdown

	weighted, err := newConsumer(sub, nrApp, cfg, log).Start(ctx, pipe.Process)
	if err != nil {
		log.Fatal("failed to start consumer", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, "sesame-rpc"); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}

	health.Serve(cfg.ListenAddr, nc.IsConnected)
	logReady(cfg, deps.Special.Len(), log)

	<-ctx.Done()
	drainInflight(weighted, cfg.DrainTimeout, log)
}

// startRefreshers launches the background automod refreshers that feed the shared
// gate: the third-party emote sets (caps false-positive suppression) and the
// optional lexicon override directory.
func startRefreshers(ctx context.Context, guard *automod.Gate, cfg *config.Config, log *zap.Logger) {
	if cfg.EmotesEnabled {
		go refreshEmotes(ctx, guard, log)
	}
	if dir := env.Get("SESAME_AUTOMOD_LEXICON_DIR", ""); dir != "" {
		go reloadLexicon(ctx, dir, guard, log)
	}
}

// logReady emits the one-line startup banner with the effective consumer tuning.
func logReady(cfg *config.Config, specialUsers int, log *zap.Logger) {
	log.Info("sesame ready",
		zap.String("consumer_name", cfg.ConsumerName),
		zap.String("premium_subject", cfg.PremiumSubject),
		zap.String("standard_subject", cfg.StandardSubject),
		zap.Int("min_routines", cfg.MinRoutines),
		zap.Int("max_routines", cfg.MaxRoutines),
		zap.Int("min_consumers", cfg.MinConsumers),
		zap.Int("max_consumers", cfg.MaxConsumers),
		zap.Int("premium_reserve_percent", cfg.PremiumReserve),
		zap.Int("special_users", specialUsers),
		zap.Duration("live_ttl", cfg.LiveTTL),
	)
}

// warnLocaleGaps logs one warning per supported locale that is missing keys, so
// a half-translated language shows up in the startup logs. Missing keys fall
// back to English at lookup time (i18n.T), so this never blocks startup; a
// declared locale with no catalog file yet reports its whole key set, capped for
// readability.
func warnLocaleGaps(log *zap.Logger) {
	for locale, missing := range i18n.Gaps() {
		if len(missing) == 0 {
			continue
		}
		log.Warn("i18n locale is missing keys; falling back to English",
			zap.String("locale", locale),
			zap.Int("missing_count", len(missing)),
			zap.Strings("missing_keys", capLocaleKeys(missing)))
	}
}

// capLocaleKeys bounds the key list logged for a locale gap so a single warning
// line stays readable when an entire catalog file is absent.
func capLocaleKeys(keys []string) []string {
	const maxKeys = 20
	if len(keys) > maxKeys {
		return keys[:maxKeys]
	}
	return keys
}

// drainInflight waits for the handlers the consumer already dispatched to run to
// completion before main returns and its deferred Close calls flush the reporters
// and shut the publishers. SIGTERM cancelled the consumer's context, so no new
// work is being pulled. A handler killed mid-flight remains unacknowledged and
// is redelivered; deterministic output IDs collapse outputs already stored.
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
