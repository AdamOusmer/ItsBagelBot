// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/dingress/internal/community"
	"ItsBagelBot/app/dingress/internal/config"
	"ItsBagelBot/app/dingress/internal/discordrate"
	"ItsBagelBot/app/dingress/internal/egress"
	"ItsBagelBot/app/dingress/internal/gateway"
	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/newrelic/go-agent/v3/newrelic"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const serviceName = "dingress"

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
	if cfg.DiscordBotToken == "" {
		// Idle path, shared by both ROLEs: an unset token means no gateway
		// Identify and no REST calls either role could make, so there is
		// nothing to wire up. Health still serves so the pod stays Ready
		// instead of crash-looping on a deliberately unconfigured deploy.
		log.Info("DISCORD_BOT_TOKEN unset; dingress idle", zap.String("role", cfg.Role))
		health.Serve(cfg.ListenAddr, serviceName)
		<-ctx.Done()
		return
	}

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	// The rate-limited REST client is shared infrastructure between ROLEs,
	// not a per-role concern: Discord's global limit is per BOT TOKEN, and
	// after this split four processes (1 gateway + 3 egress) share one
	// token. Both roles build one of these off their own Valkey client, but
	// the bucket they pay into (ratelimit:discord:global) is the same key
	// for all four -- see internal/discordrate's package doc.
	rest := discordrate.NewLimitedClient(discordapi.NewClient(cfg.DiscordBotToken), discordrate.New(valkeyClient))

	if cfg.Role == config.RoleEgress {
		runEgress(ctx, cfg, log, nrApp, valkeyClient, rest)
		return
	}
	runGateway(ctx, cfg, log, valkeyClient, rest)
}

// runGateway is exactly today's dingress behavior: the single Discord
// gateway Identify session driving welcomes, join-to-create voice, tickets,
// staff logs, crumb ranks, and slash commands. No NATS. Deployed at
// replicas: 1 -- two Identify sessions on one bot token fight each other.
func runGateway(ctx context.Context, cfg config.Config, log *zap.Logger, valkeyClient valkey_go.Client, rest *discordrate.LimitedClient) {
	bot := &community.Bot{
		REST:    rest,
		Store:   store.New(valkeyClient),
		Modules: community.ProjectionModules{Src: projection.NewStore(valkeyClient)},
		Log:     log,
	}

	health.Serve(cfg.ListenAddr, serviceName)

	sess := gateway.Session{
		Token:  cfg.DiscordBotToken,
		Dial:   gateway.DialWS,
		Handle: bot,
		Log:    log,
	}
	if err := sess.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal("discord gateway stopped", zap.Error(err))
	}
	log.Info("dingress shutting down")
}

// runEgress is the outbound half moved out of app/outgress: no gateway
// session, NATS-driven, safe at any replica count. It consumes
// twitch.ingress.event.stream (go-live/offline embeds) and
// data.twitch.clip.created (the clip archive post), and serves the guild
// setup/layout/unbind/post RPC.
func runEgress(ctx context.Context, cfg config.Config, log *zap.Logger, nrApp *newrelic.Application, valkeyClient valkey_go.Client, rest *discordrate.LimitedClient) {
	// dingress ensures NOTHING. It is a pure consumer of two streams it does
	// not own: BAGEL_DATA belongs to users, TWITCH_INGRESS to ingress and the
	// projector. Reconciling BAGEL_DATA here looked harmless -- every peer
	// converges on an identical no-op update -- but the ACL disagrees: this
	// account holds consumer grants only, deliberately, so an EnsureStreams
	// call would need $JS.API.STREAM.CREATE/UPDATE it must not have.
	// TestRuntimeStreamOwnershipMatchesACL in deploy/messaging enforces that a
	// service reconciling a stream also owns it, and caught exactly this.
	//
	// Not ensuring also drops a boot dependency: if BAGEL_DATA is missing,
	// dingress waits on its owner rather than Fatal-ing in a crash loop.
	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	projStore := projection.NewStore(valkeyClient)
	w := egress.New(egress.Config{
		Discord:     rest,
		DiscordKV:   egress.NewLiveStore(valkeyClient),
		DiscordMods: projStore,
		StreamInfo:  projStore,
		Log:         log,
	})

	closeConsumers := startEgressConsumers(ctx, cfg, nrApp, log, w)
	defer closeConsumers()

	if err := egress.SubscribeRPC(w, egress.RPCWiring{
		NC: nc, Prefix: cfg.RPCPrefix, Queue: cfg.RPCQueue, App: nrApp, Log: log.Named("rpc"),
	}); err != nil {
		log.Fatal("failed to subscribe discord guild rpc", zap.Error(err))
	}

	health.Serve(cfg.ListenAddr, serviceName, health.NATS("nats", nc))

	log.Info("dingress egress ready",
		zap.String("stream_subject", cfg.StreamLaneSubject),
		zap.String("clip_subject", cfg.ClipCreatedSubject),
		zap.String("rpc_prefix", cfg.RPCPrefix))

	<-ctx.Done()
	log.Info("dingress shutting down")
}

// startEgressConsumers binds the two egress inputs on one shared durable
// subscriber. One Subscriber spanning both TWITCH_INGRESS and BAGEL_DATA
// subjects is the established pattern (see app/projector/main.go's
// registerConsumers, which folds subjects from both streams through a
// single bound subscriber).
func startEgressConsumers(ctx context.Context, cfg config.Config, nrApp *newrelic.Application, log *zap.Logger, w *egress.Worker) func() {
	sub, err := bus.NewSubscriber(cfg.NATSURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect egress subscriber", zap.Error(err))
	}
	if err := bus.Consume(ctx, nrApp, sub, cfg.StreamLaneSubject, w.HandleStreamEvent, log); err != nil {
		log.Fatal("failed to consume stream lane", zap.Error(err))
	}
	if err := bus.Consume(ctx, nrApp, sub, cfg.ClipCreatedSubject, w.HandleClipCreated, log); err != nil {
		log.Fatal("failed to consume clip-created lane", zap.Error(err))
	}
	return func() { _ = sub.Close() }
}
