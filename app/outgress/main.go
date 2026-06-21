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
	"ItsBagelBot/app/outgress/internal/ratelimit"
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

// Nacked messages (rate limited, Twitch down, paused) come back after
// nakDelay and die after maxRedeliveries, so a chat message lives at most
// ~90 seconds; later than that it is worthless anyway.
const (
	nakDelay        = 2 * time.Second
	maxRedeliveries = 45
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

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	limiter := ratelimit.New(valkeyClient)
	registry := channels.New(valkeyClient)

	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

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

	conduitResolver := conduit.New(nc, cfg.ConduitSubject, cfg.TwitchConduitID, 60*time.Second, log.Named("conduit"))

	premium := worker.New(log.Named("premium"), limiter, registry, tw, cfg.TwitchBotUserID, conduitResolver, worker.LanePremium)
	standard := worker.New(log.Named("standard"), limiter, registry, tw, cfg.TwitchBotUserID, conduitResolver, worker.LaneStandard)
	// The system lane carries the dashboard's EventSub create/delete jobs; it
	// pays only the reserved system Helix partition, so onboarding bursts
	// never compete with chat/api traffic for the general budget.
	system := worker.New(log.Named("system"), limiter, registry, tw, cfg.TwitchBotUserID, conduitResolver, worker.LaneSystem)

	// One durable group per lane so each lane drains independently; the
	// paced redelivery keeps rate-limit nacks from spinning.
	premiumSub, err := bus.NewLaneSubscriber(cfg.NATSURL, "outgress-premium", nakDelay, maxRedeliveries, log)
	if err != nil {
		log.Fatal("failed to connect premium subscriber", zap.Error(err))
	}
	defer func() { _ = premiumSub.Close() }()

	standardSub, err := bus.NewLaneSubscriber(cfg.NATSURL, "outgress-standard", nakDelay, maxRedeliveries, log)
	if err != nil {
		log.Fatal("failed to connect standard subscriber", zap.Error(err))
	}
	defer func() { _ = standardSub.Close() }()

	systemSub, err := bus.NewLaneSubscriber(cfg.NATSURL, "outgress-system", nakDelay, maxRedeliveries, log)
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

	if err := rpc.SubscribeManage(nc, registry, tw, cfg.RPCPrefix, "outgress-rpc", nrApp, log.Named("rpc")); err != nil {
		log.Fatal("failed to subscribe management rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("outgress ready",
		zap.String("premium_subject", cfg.PremiumSubject),
		zap.String("standard_subject", cfg.StandardSubject),
		zap.String("rpc_prefix", cfg.RPCPrefix),
		zap.Bool("mod_verification", tw.HasUserToken()),
		zap.Int("min_routines", cfg.MinRoutines),
		zap.Int("max_routines", cfg.MaxRoutines),
		zap.Int("max_consumers", cfg.MaxConsumers),
		zap.Int("premium_reserve_percent", cfg.PremiumReserve),
		zap.Int("system_workers", cfg.SystemWorkers))

	<-ctx.Done()

	log.Info("outgress shutting down")
}
