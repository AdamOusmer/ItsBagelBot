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

	subscribeRPCs(rpcDeps{
		NC: nc, Cfg: cfg, Rest: rest, Store: store,
		LiveStore: liveStore, Reauth: reauth, NRApp: nrApp, Log: log,
	})

	lanes := startCommandConsumer(consumerDeps{
		Ctx: ctx, Cfg: cfg, Rest: rest, ApplicationID: applicationID, Reauth: reauth, Log: log,
	})
	defer lanes.Close()

	health.ServeSet(cfg.ListenAddr, healthSet(nc, lanes, log))
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

// rpcDeps is subscribeRPCs's whole input, collapsed from eight positional
// parameters into one struct (CodeScene: Excess Number of Function
// Arguments, over its 4-parameter limit).
type rpcDeps struct {
	NC        *nats.Conn
	Cfg       config.Config
	Rest      *discordrate.LimitedClient
	Store     discordstore.Store
	LiveStore kv.LiveStore
	Reauth    kv.ReauthStore
	NRApp     *newrelic.Application
	Log       *zap.Logger
}

// subscribeRPCs wires the dashboard-facing guild setup RPC and the
// engine-facing channel-management/live RPC onto the same connection.
func subscribeRPCs(deps rpcDeps) {
	setupWorker := setup.New(setup.Config{Discord: deps.Rest, Store: deps.Store, Log: deps.Log.Named("setup")})
	if err := rpc.SubscribeSetup(setupWorker, rpc.SetupWiring{
		NC: deps.NC, Prefix: deps.Cfg.RPCPrefix, Queue: deps.Cfg.RPCQueue, App: deps.NRApp,
		Reauth: deps.Reauth, Log: deps.Log.Named("rpc"),
	}); err != nil {
		deps.Log.Fatal("failed to subscribe discord guild setup rpc", zap.Error(err))
	}
	if err := rpc.SubscribeEngine(deps.Rest, deps.LiveStore, rpc.EngineWiring{
		NC: deps.NC, Prefix: deps.Cfg.DiscordEngineRPCPrefix, Queue: deps.Cfg.DiscordEngineRPCQueue, App: deps.NRApp, Log: deps.Log.Named("engine-rpc"),
	}); err != nil {
		deps.Log.Fatal("failed to subscribe discord engine rpc", zap.Error(err))
	}
}

// consumerDeps is what starting the command consumer needs, as one value
// rather than six positional parameters. Same reason as rpcDeps above: a
// six-argument call, four of which are pointers or interfaces, has several
// orderings that compile and one that is correct.
type consumerDeps struct {
	Ctx           context.Context
	Cfg           config.Config
	Rest          *discordrate.LimitedClient
	ApplicationID string
	Reauth        kv.ReauthStore
	Log           *zap.Logger
}

// startCommandConsumer returns the bound lanes, not just their close func:
// healthSet reports on each of them by name.
func startCommandConsumer(deps consumerDeps) commands.Lanes {
	log := deps.Log.Named("commands")
	handlers := &commands.Handlers{
		Rest: deps.Rest, ApplicationID: deps.ApplicationID, Reauth: deps.Reauth, Log: log,
	}
	consumer := &commands.Consumer{NATSURL: deps.Cfg.NATSURL, Log: log, Handle: handlers.Dispatch}
	lanes, err := consumer.Run(deps.Ctx)
	if err != nil {
		deps.Log.Fatal("failed to start discord command consumer", zap.Error(err))
	}
	return lanes
}

// healthSet is outgress's own report plus the RPC responder that serves it, the
// one engine folds into health.itsbagelbot.com/discord -- outgress is not
// routed from outside, so this RPC is the only way its verdict reaches the
// vertical's answer.
//
// The two lane checks are the reason this is a Set and not a lone NATS check.
// Draining DISCORD_OUTGRESS is the entire job of this process, and a durable
// that is still bound but no longer fetching leaves the connection check green
// while every Command silently ages out at the stream's 60s MaxAge. Checked per
// lane so the report names which of mod/default wedged, since mod-first
// priority means the two fail independently.
func healthSet(nc *nats.Conn, lanes commands.Lanes, log *zap.Logger) *health.Set {
	set := health.NewSet(serviceName,
		health.NATS("nats", nc),
		bus.LaneCheck("mod", lanes.Mod),
		bus.LaneCheck("default", lanes.Default),
	)
	rpcCheck, err := bus.SubscribeRPCHealth(nc, serviceName, serviceName+"-rpc", set)
	if err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}
	set.Add(rpcCheck)
	return set
}
