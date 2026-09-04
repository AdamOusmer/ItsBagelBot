// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Command outgress is the Discord half of the ingress/engine/outgress split
// that acts: it is the only process in the split holding a Discord REST
// client. It consumes both DISCORD_OUTGRESS lanes (mod drained before
// default, see internal/commands), serves the dashboard-facing guild
// setup/layout/unbind/post RPC (unchanged wire contract from app/dingress's
// ROLE=egress), and serves the new engine-facing channel-management/live RPC
// (see internal/domain/rpc/discordoutgress). No gateway Identify session, so
// it is safe at any replica count.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/discord/outgress/internal/bootstrap"
	"ItsBagelBot/app/discord/outgress/internal/commands"
	"ItsBagelBot/app/discord/outgress/internal/config"
	"ItsBagelBot/app/discord/outgress/internal/kv"
	"ItsBagelBot/app/discord/outgress/internal/rpc"
	"ItsBagelBot/app/discord/outgress/internal/setup"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordrate"
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const serviceName = "discord-outgress"

// main reads as a sequence of named boot phases; each phase logs and exits
// the process on its own fatal error (matching what this used to do inline)
// so the phase order below is also the exact fatal-error order.
func main() {
	log, nrApp := bootLogger()
	defer func() { _ = log.Sync() }()
	defer monitor.Shutdown(nrApp)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	if cfg.DiscordBotToken == "" {
		log.Info("DISCORD_BOT_TOKEN unset; discord outgress idle")
		health.Serve(cfg.ListenAddr, serviceName)
		<-ctx.Done()
		return
	}

	ensureOutgressStream(ctx, cfg, log)

	valkeyClient := connectValkey(cfg, log)
	defer valkeyClient.Close()

	rest := discordrate.NewLimitedClient(discordapi.NewClient(cfg.DiscordBotToken), discordrate.New(valkeyClient))
	store := discordstore.New(valkeyClient)
	liveStore := kv.New(valkeyClient)
	reauth := kv.NewReauthStore(valkeyClient)

	applicationID := registerSlashCommands(ctx, rest, log)

	nc := connectNATS(cfg, log)
	defer nc.Close()

	subscribeRPCs(nc, cfg, rest, store, liveStore, reauth, nrApp, log)

	closeCommands := startCommandConsumer(ctx, cfg, rest, applicationID, reauth, log)
	defer closeCommands()

	health.Serve(cfg.ListenAddr, serviceName, health.NATS("nats", nc))
	log.Info("discord outgress ready", zap.String("application_id", applicationID), zap.String("rpc_prefix", cfg.RPCPrefix))

	<-ctx.Done()
	log.Info("discord outgress shutting down")
}

// bootLogger builds the process logger wired through New Relic. It exits the
// process on failure: nothing after this point can run without a logger.
func bootLogger() (*zap.Logger, *newrelic.Application) {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	nrApp, err := monitor.New(serviceName, log)
	if err != nil {
		log.Fatal("failed to start new relic", zap.Error(err))
	}
	return monitor.WrapLogger(log, nrApp), nrApp
}

// ensureOutgressStream provisions DISCORD_OUTGRESS, the stream this process
// consumes (see pkg/bus.DiscordOutgressStream's doc); it never provisions
// DISCORD_INGRESS, which it only ever reads nothing from at all -- that is
// engine's job, as the consumer on that side.
func ensureOutgressStream(ctx context.Context, cfg config.Config, log *zap.Logger) {
	if err := bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.DiscordOutgressStream}, log); err != nil {
		log.Fatal("failed to provision the DISCORD_OUTGRESS stream", zap.Error(err))
	}
}

func connectValkey(cfg config.Config, log *zap.Logger) valkey.Client {
	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	return valkeyClient
}

// registerSlashCommands is not fatal on failure: a stale slash-command
// catalog or a missing application id degrades interaction followups and
// re-registration, it does not stop the mod/default lanes from draining.
// Retried next rollout.
func registerSlashCommands(ctx context.Context, rest *discordrate.LimitedClient, log *zap.Logger) string {
	applicationID, err := bootstrap.Register(ctx, rest)
	if err != nil {
		log.Warn("discord slash-command bootstrap failed", zap.Error(err))
	}
	return applicationID
}

func connectNATS(cfg config.Config, log *zap.Logger) *nats.Conn {
	nc, err := bus.Connect(cfg.NATSRPCURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	return nc
}

// subscribeRPCs wires the dashboard-facing guild setup RPC and the
// engine-facing channel-management/live RPC onto the same connection.
func subscribeRPCs(
	nc *nats.Conn, cfg config.Config, rest *discordrate.LimitedClient,
	store discordstore.Store, liveStore kv.LiveStore, reauth kv.ReauthStore,
	nrApp *newrelic.Application, log *zap.Logger,
) {
	setupWorker := setup.New(setup.Config{Discord: rest, Store: store, Log: log.Named("setup")})
	if err := rpc.SubscribeSetup(setupWorker, rpc.SetupWiring{
		NC: nc, Prefix: cfg.RPCPrefix, Queue: cfg.RPCQueue, App: nrApp,
		Reauth: reauth, Log: log.Named("rpc"),
	}); err != nil {
		log.Fatal("failed to subscribe discord guild setup rpc", zap.Error(err))
	}
	if err := rpc.SubscribeEngine(rest, liveStore, rpc.EngineWiring{
		NC: nc, Prefix: cfg.DiscordEngineRPCPrefix, Queue: cfg.DiscordEngineRPCQueue, App: nrApp, Log: log.Named("engine-rpc"),
	}); err != nil {
		log.Fatal("failed to subscribe discord engine rpc", zap.Error(err))
	}
}

func startCommandConsumer(
	ctx context.Context, cfg config.Config, rest *discordrate.LimitedClient,
	applicationID string, reauth kv.ReauthStore, log *zap.Logger,
) func() {
	handlers := &commands.Handlers{Rest: rest, ApplicationID: applicationID, Reauth: reauth, Log: log.Named("commands")}
	consumer := &commands.Consumer{NATSURL: cfg.NATSURL, Log: log.Named("commands"), Handle: handlers.Dispatch}
	closeCommands, err := consumer.Run(ctx)
	if err != nil {
		log.Fatal("failed to start discord command consumer", zap.Error(err))
	}
	return closeCommands
}
