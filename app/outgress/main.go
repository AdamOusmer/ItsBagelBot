package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
	"ItsBagelBot/app/outgress/internal/conduit"
	"ItsBagelBot/app/outgress/internal/config"
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/app/outgress/internal/tokenstore"
	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/app/outgress/internal/worker"
	"ItsBagelBot/app/outgress/rpc"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const serviceName = "outgress"

// A failed command is retried three times at one-second intervals. The
// work-queue stream also has a five-second MaxAge, so it cannot survive a
// restart and reappear later as stale chat output.
const (
	nakDelay        = time.Second
	maxRedeliveries = 3
)

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
	// The deployment supplies a stable locality for the quota-lease protocol.
	// Keep the config fallback usable so a missing optional tuning value cannot
	// turn an otherwise healthy outgress rollout into a fleet-wide outage.
	if os.Getenv("OUTGRESS_REGION") == "" {
		log.Warn("OUTGRESS_REGION is unset; using fallback locality",
			zap.String("rate_region", cfg.RateRegion))
	}
	if err := worker.PrepareJSON(); err != nil {
		log.Warn("failed to precompile outgress JSON decoders", zap.Error(err))
	}

	// Reconcile both outgress streams here (not only from producer services) so
	// their retention and lifetimes are guaranteed before any lane consumer
	// attaches. Order matters: the chat stream is narrowed off the system subject
	// FIRST, so adding the system stream cannot overlap it. The chat lanes are
	// perishable work-queue (5s); the control lane keeps a longer lifetime so an
	// EventSub enroll survives a rollout gap instead of being purged.
	if err := bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.OutgressStream, bus.OutgressSystemStream}, log); err != nil {
		log.Fatal("failed to provision outgress streams", zap.Error(err))
	}

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	centralLimiter := ratelimit.New(valkeyClient)
	registry := channels.New(valkeyClient)

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()
	if err := registry.StartInvalidationListener(nc, cfg.CacheInvalidatePrefix, log.Named("channels")); err != nil {
		log.Fatal("failed to subscribe channel cache invalidation", zap.Error(err))
	}
	defer registry.Close()

	appTokens := twitch.NewAppTokenSource(cfg.TwitchClientID, cfg.TwitchClientSecret)

	// The bot account's user token prefers the copy stored by the users
	// service (the admin panel manages it); the env refresh token is only a
	// seed or, without a bot user id, the legacy static configuration.
	var userTokens *twitch.Source
	switch {
	case cfg.TwitchBotUserID != "":
		store := tokenstore.New(nc, cfg.TokensSubjectPrefix, cfg.TwitchBotUserID)
		userTokens = twitch.NewStoredUserTokenSource(
			cfg.TwitchClientID, cfg.TwitchClientSecret, cfg.TwitchBotRefreshToken,
			func(ctx context.Context) string {
				refresh, err := store.Load(ctx)
				if err != nil {
					log.Debug("stored bot token unavailable", zap.Error(err))
					return ""
				}
				return refresh
			},
			func(ctx context.Context, access, refresh string) {
				if err := store.Save(ctx, access, refresh); err != nil {
					log.Warn("bot token persist failed", zap.Error(err))
				}
			},
		)
	case cfg.TwitchBotRefreshToken != "":
		userTokens = twitch.NewUserTokenSource(cfg.TwitchClientID, cfg.TwitchClientSecret, cfg.TwitchBotRefreshToken)
	default:
		log.Warn("no bot user id or refresh token configured, mod status verification disabled")
	}

	// Per-broadcaster user tokens: a job with as="broadcaster" sends under the
	// channel's own stored grant (saved by the dashboard at login) rather than
	// the bot. Each Source loads/persists that channel's refresh token through
	// the same users-service token RPC, keyed by broadcaster id.
	broadcasterTokens := twitch.NewBroadcasterTokens(func(broadcasterID string) *twitch.Source {
		store := tokenstore.New(nc, cfg.TokensSubjectPrefix, broadcasterID)
		return twitch.NewStoredUserTokenSource(
			cfg.TwitchClientID, cfg.TwitchClientSecret, "",
			func(ctx context.Context) string {
				refresh, err := store.Load(ctx)
				if err != nil {
					log.Debug("broadcaster token unavailable", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
					return ""
				}
				return refresh
			},
			func(ctx context.Context, access, refresh string) {
				if err := store.Save(ctx, access, refresh); err != nil {
					log.Warn("broadcaster token persist failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
				}
			},
		)
	})

	tw := twitch.NewClient(cfg.TwitchClientID, appTokens, userTokens, broadcasterTokens)
	defer tw.CloseIdleConnections()

	// Token minting, DNS/TLS and the first HTTP/2 handshake used to land on the
	// first real chat message handled by each new pod. Pay that cold-start cost
	// before consumers and readiness come online. A transient Twitch outage must
	// not crash-loop the service, so the bounded warmup degrades to a warning.
	warmupStarted := time.Now()
	warmupCtx, warmupCancel := context.WithTimeout(ctx, 8*time.Second)
	warmupErr := tw.Warmup(warmupCtx)
	warmupCancel()
	if warmupErr != nil {
		log.Warn("twitch warmup failed; continuing with lazy retry",
			zap.Duration("duration", time.Since(warmupStarted)), zap.Error(warmupErr))
	} else {
		log.Info("twitch client warmed", zap.Duration("duration", time.Since(warmupStarted)))
	}

	conduitResolver := conduit.New(nc, cfg.ConduitSubject, cfg.TwitchConduitID, 60*time.Second, log.Named("conduit"))

	// Stable pod identity is used only for lease membership and targeted permits;
	// it never assigns broadcaster ownership.
	host, err := os.Hostname()
	if err != nil || host == "" {
		log.Fatal("failed to determine outgress pod identity", zap.Error(err))
	}
	// Label every worker transaction with this pod's region and the Kubernetes
	// node it runs on so the Twitch external-segment duration can be split per
	// node in New Relic. NODE_NAME (spec.nodeName) names the actual node;
	// hostname (the pod) is the dev fallback.
	worker.SetNodeIdentity(cfg.RateRegion, env.Get("NODE_NAME", host))

	buckets := ratelimit.NewBucketStore(10_000)

	permitSvc, err := ratelimit.NewPermitService(nc, cfg.RateRegion, host, buckets)
	if err != nil {
		log.Fatal("failed to initialize permit service", zap.Error(err))
	}
	defer permitSvc.Close()

	limiter := ratelimit.NewLeaseManager(centralLimiter, buckets, permitSvc,
		ratelimit.WithLeaseIdentity(cfg.RateRegion, host))
	permitSvc.SetGrantor(limiter)
	coordinator := ratelimit.NewLeaseCoordinator(nc, valkeyClient, limiter, cfg.RateRegion, host,
		ratelimit.CoordinatorConfig{
			Epoch: cfg.LeaseEpoch, Guard: cfg.LeaseGuard, MinMembers: cfg.LeaseMinMembers,
			Replicas: cfg.LeaseReplicas, ReplicaTimeout: cfg.LeaseReplicaTimeout,
		}, log.Named("leases"))
	if err := coordinator.Start(ctx); err != nil {
		log.Fatal("failed to initialize lease coordinator", zap.Error(err))
	}
	defer coordinator.Close()

	premium := worker.New(log.Named("premium"), limiter, registry, tw, cfg.TwitchBotUserID, host, conduitResolver, worker.LanePremium)
	standard := worker.New(log.Named("standard"), limiter, registry, tw, cfg.TwitchBotUserID, host, conduitResolver, worker.LaneStandard)
	// The system lane carries the dashboard's EventSub create/delete jobs; it
	// pays only the reserved system Helix partition, so onboarding bursts
	// never compete with chat/api traffic for the general budget.
	system := worker.New(log.Named("system"), limiter, registry, tw, cfg.TwitchBotUserID, host, conduitResolver, worker.LaneSystem)
	modVerifier := worker.NewModVerifier(registry, tw, cfg.TwitchBotUserID, host, log.Named("mod-status"))
	defer modVerifier.Close()
	premium.SetModVerifier(modVerifier)
	standard.SetModVerifier(modVerifier)
	system.SetModVerifier(modVerifier)
	// The system lane also resolves live re-checks (stream_status jobs) and writes
	// the result back into the live projection for the worker fleet.
	system.SetLiveWriter(worker.NewLiveWriter(valkeyClient, nc, cfg.CacheInvalidatePrefix, cfg.LiveTTL, log.Named("live")))

	// paced redelivery keeps rate-limit nacks from spinning.
	premiumSub, err := bus.NewLaneSubscriber(cfg.NATSURL, bus.OutgressStream.Name, cfg.PremiumSubject, "outgress-premium", []time.Duration{nakDelay}, maxRedeliveries, log)
	if err != nil {
		log.Fatal("failed to connect premium subscriber", zap.Error(err))
	}
	defer func() { _ = premiumSub.Close() }()

	standardSub, err := bus.NewLaneSubscriber(cfg.NATSURL, bus.OutgressStream.Name, cfg.StandardSubject, "outgress-standard", []time.Duration{nakDelay}, maxRedeliveries, log)
	if err != nil {
		log.Fatal("failed to connect standard subscriber", zap.Error(err))
	}
	defer func() { _ = standardSub.Close() }()

	systemBackoff := []time.Duration{
		15 * time.Second,
		time.Minute,
		3 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
	}
	systemSub, err := bus.NewLaneSubscriber(cfg.NATSURL, bus.OutgressSystemStream.Name, cfg.SystemSubject, "outgress-system", systemBackoff, uint64(len(systemBackoff)), log)
	if err != nil {
		log.Fatal("failed to connect system subscriber", zap.Error(err))
	}
	defer func() { _ = systemSub.Close() }()

	// Premium and standard share one central weighted consumer: a single
	// routine budget partitioned by weight so premium drains ahead without
	// starving standard.
	if err := bus.ConsumeWeighted(ctx, nrApp, []bus.WeightedLane{
		{Sub: premiumSub, Subject: cfg.PremiumSubject, Handle: premium.Process, Reserve: cfg.PremiumReserve},
		{Sub: standardSub, Subject: cfg.StandardSubject, Handle: standard.Process},
	}, bus.ScalePolicy{
		MinRoutines:    cfg.MinRoutines,
		MaxRoutines:    cfg.MaxRoutines,
		MaxConsumers:   cfg.MaxConsumers,
		ScaleUpAfter:   cfg.ScaleUpAfter,
		ScaleDownAfter: cfg.ScaleDownAfter,
	}, log); err != nil {
		log.Fatal("failed to consume premium/standard lanes", zap.Error(err))
	}

	// The system lane keeps its own independent consumer, off the weighted
	// budget, so onboarding bursts never compete for the chat/api routines. It
	// runs a fixed pool (min == max, single consumer), no autoscaling.
	if err := bus.ConsumeWeighted(ctx, nrApp, []bus.WeightedLane{
		{Sub: systemSub, Subject: cfg.SystemSubject, Handle: system.Process},
	}, bus.ScalePolicy{
		MinRoutines:  cfg.SystemWorkers,
		MaxRoutines:  cfg.SystemWorkers,
		MaxConsumers: 1,
	}, log); err != nil {
		log.Fatal("failed to consume system lane", zap.Error(err))
	}

	// Real Twitch stream.online / stream.offline events flow on the ingress
	// stream lane (TWITCH_INGRESS, provisioned by ingress/projector). Bind a
	// durable consumer under outgress's OWN service group so the system worker
	// re-verifies the bot's mod status on every go-live. This restores the
	// re-verify that used to ride the cold-live escalation: once the projector
	// writes the live key directly from these events, the worker's live query is
	// no longer cold, so stream_status (and its mod-status re-check) no longer
	// fires. The projector binds its own group on the same subject and still
	// gets every event once. Best-effort and idempotent: HandleStreamEvent only
	// re-verifies, never writes live state (that is the projector's job).
	streamSub, err := bus.NewSubscriber(cfg.NATSURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect stream-lane subscriber", zap.Error(err))
	}
	defer func() { _ = streamSub.Close() }()

	if err := bus.Consume(ctx, nrApp, streamSub, cfg.StreamLaneSubject, system.HandleStreamEvent, log); err != nil {
		log.Fatal("failed to consume stream lane", zap.Error(err))
	}

	if err := rpc.SubscribeManage(nc, registry, tw, cfg.RPCPrefix, "outgress-rpc", nrApp, log.Named("rpc")); err != nil {
		log.Fatal("failed to subscribe management rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("outgress ready",
		zap.String("premium_subject", cfg.PremiumSubject),
		zap.String("standard_subject", cfg.StandardSubject),
		zap.String("rpc_prefix", cfg.RPCPrefix),
		zap.String("stream_lane_subject", cfg.StreamLaneSubject),
		zap.Bool("mod_verification", tw.HasUserToken()),
		zap.Int("min_routines", cfg.MinRoutines),
		zap.Int("max_routines", cfg.MaxRoutines),
		zap.Int("max_consumers", cfg.MaxConsumers),
		zap.Int("premium_reserve_percent", cfg.PremiumReserve),
		zap.Int("system_workers", cfg.SystemWorkers))

	<-ctx.Done()

	log.Info("outgress shutting down")
}
