package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ItsBagelBot/app/outgress/internal/channels"
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

	"github.com/valkey-io/valkey-go"

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

	valkeyOpts := valkey.ClientOption{
		InitAddress:  []string{cfg.ValkeyAddr},
		Password:     cfg.ValkeyPassword,
		DisableCache: true,
	}
	if strings.HasSuffix(cfg.ValkeyAddr, ":26379") {
		valkeyOpts.Sentinel = valkey.SentinelOption{MasterSet: "myprimary"}
	}
	valkeyClient, err := valkey.NewClient(valkeyOpts)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	limiter := ratelimit.New(valkeyClient)
	registry := channels.New(valkeyClient)

	nc, err := bus.Connect(cfg.NATSURL, serviceName)
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

	tw := twitch.NewClient(cfg.TwitchClientID, appTokens, userTokens)

	premium := worker.New(log.Named("premium"), limiter, registry, tw, cfg.TwitchBotUserID, cfg.TwitchConduitID, worker.LanePremium)
	standard := worker.New(log.Named("standard"), limiter, registry, tw, cfg.TwitchBotUserID, cfg.TwitchConduitID, worker.LaneStandard)
	// The system lane carries the dashboard's EventSub create/delete jobs; it
	// pays only the reserved system Helix partition, so onboarding bursts
	// never compete with chat/api traffic for the general budget.
	system := worker.New(log.Named("system"), limiter, registry, tw, cfg.TwitchBotUserID, cfg.TwitchConduitID, worker.LaneSystem)

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

	if err := bus.Consume(ctx, nrApp, premiumSub, cfg.PremiumSubject, premium.Process, log); err != nil {
		log.Fatal("failed to consume premium lane", zap.Error(err))
	}

	if err := bus.Consume(ctx, nrApp, standardSub, cfg.StandardSubject, standard.Process, log); err != nil {
		log.Fatal("failed to consume standard lane", zap.Error(err))
	}

	if err := bus.Consume(ctx, nrApp, systemSub, cfg.SystemSubject, system.Process, log); err != nil {
		log.Fatal("failed to consume system lane", zap.Error(err))
	}

	if err := rpc.SubscribeManage(nc, registry, tw, cfg.RPCPrefix, "outgress-rpc", log.Named("rpc")); err != nil {
		log.Fatal("failed to subscribe management rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("outgress ready",
		zap.String("premium_subject", cfg.PremiumSubject),
		zap.String("standard_subject", cfg.StandardSubject),
		zap.String("rpc_prefix", cfg.RPCPrefix),
		zap.Bool("mod_verification", tw.HasUserToken()))

	<-ctx.Done()

	log.Info("outgress shutting down")
}
