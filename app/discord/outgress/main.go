// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/discord/outgress/internal/bootstrap"
	"ItsBagelBot/app/discord/outgress/internal/commands"
	"ItsBagelBot/app/discord/outgress/internal/config"
	"ItsBagelBot/app/discord/outgress/internal/kv"
	"ItsBagelBot/app/discord/outgress/internal/rpc"
	"ItsBagelBot/app/discord/outgress/internal/setup"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordboot"
	"ItsBagelBot/internal/discordrate"
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const serviceName = "discord-outgress"

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx, nrApp := core.Log, core.Ctx, core.NR

	cfg := config.Load()
	idle := discordboot.Service{Name: serviceName, Token: cfg.DiscordBotToken, Listen: cfg.ListenAddr}
	if discordboot.IdleIfNoToken(core, idle) {
		return
	}

	ensureOutgressStream(ctx, cfg, log)

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()

	rest := discordrate.NewClient(cfg.DiscordBotToken, discordrate.New(valkeyClient))
	liveStore := kv.New(valkeyClient)
	reauth := kv.NewReauthStore(valkeyClient)
	botStatus := kv.NewBotStatusReader(valkeyClient)
	lockdowns := kv.NewLockdownStore(valkeyClient)

	applicationID := registerSlashCommands(ctx, rest, log)

	nc := svcboot.MustRPCConn(core, cfg.NATSRPCURL)
	defer nc.Close()

	store := discordstore.NewRPC(nc, cfg.DiscordDataRPCPrefix, valkeyClient, log)

	subscribeRPCs(rpcDeps{
		NC: nc, Cfg: cfg, Rest: rest, Store: store, ApplicationID: applicationID,
		LiveStore: liveStore, Reauth: reauth, BotStatus: botStatus, NRApp: nrApp, Log: log,
	})

	lanes := startCommandConsumer(consumerDeps{
		Ctx: ctx, Cfg: cfg, Rest: rest, ApplicationID: applicationID,
		Reauth: reauth, Lockdowns: lockdowns, Log: log,
	})
	defer lanes.Close()

	svcboot.ServeHealth(svcboot.Health{
		Log: log, NC: nc, Service: serviceName, QueueGroup: serviceName + "-rpc", ListenAddr: cfg.ListenAddr,
	}, laneChecks(lanes)...)
	log.Info("discord outgress ready", zap.String("application_id", applicationID), zap.String("rpc_prefix", cfg.RPCPrefix))

	core.Await()
}

func ensureOutgressStream(ctx context.Context, cfg config.Config, log *zap.Logger) {
	svcboot.FatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.DiscordOutgressStream}, log),
		"failed to provision the DISCORD_OUTGRESS stream")
}

func registerSlashCommands(ctx context.Context, rest *discordapi.Client, log *zap.Logger) string {
	applicationID, err := bootstrap.Register(ctx, rest)
	if err != nil {
		log.Warn("discord slash-command bootstrap failed", zap.Error(err))
	}
	return applicationID
}

type rpcDeps struct {
	NC            *nats.Conn
	Cfg           config.Config
	Rest          *discordapi.Client
	Store         discordstore.Store
	ApplicationID string
	LiveStore     kv.LiveStore
	Reauth        kv.ReauthStore
	BotStatus     kv.BotStatusReader
	NRApp         *newrelic.Application
	Log           *zap.Logger
}

func subscribeRPCs(deps rpcDeps) {
	setupWorker := setup.New(setup.Config{Discord: deps.Rest, Store: deps.Store, Log: deps.Log.Named("setup")})
	setupWiring := rpc.SetupWiring{
		Wiring: rpc.Wiring{
			NC: deps.NC, Prefix: deps.Cfg.RPCPrefix, Queue: deps.Cfg.RPCQueue,
			App: deps.NRApp, Log: deps.Log.Named("rpc"),
		},
		Reauth: deps.Reauth, Status: deps.BotStatus,
	}
	if err := rpc.SubscribeSetup(setupWorker, setupWiring); err != nil {
		deps.Log.Fatal("failed to subscribe discord guild setup rpc", zap.Error(err))
	}
	engineWiring := rpc.Wiring{
		NC: deps.NC, Prefix: deps.Cfg.DiscordEngineRPCPrefix, Queue: deps.Cfg.DiscordEngineRPCQueue,
		App: deps.NRApp, Log: deps.Log.Named("engine-rpc"),
	}
	if err := rpc.SubscribeEngine(deps.Rest, deps.LiveStore, engineWiring); err != nil {
		deps.Log.Fatal("failed to subscribe discord engine rpc", zap.Error(err))
	}
	ticketDeps := rpc.TicketDeps{Memo: deps.Store, BotID: deps.ApplicationID}
	if err := rpc.SubscribeTickets(deps.Rest, ticketDeps, engineWiring); err != nil {
		deps.Log.Fatal("failed to subscribe discord ticket rpc", zap.Error(err))
	}
}

type consumerDeps struct {
	Ctx           context.Context
	Cfg           config.Config
	Rest          *discordapi.Client
	ApplicationID string
	Reauth        kv.ReauthStore
	Lockdowns     kv.LockdownStore
	Log           *zap.Logger
}

func startCommandConsumer(deps consumerDeps) commands.Lanes {
	log := deps.Log.Named("commands")
	handlers := &commands.Handlers{
		Rest: deps.Rest, ApplicationID: deps.ApplicationID,
		Reauth: deps.Reauth, Lockdown: deps.Lockdowns, Log: log,
	}
	consumer := &commands.Consumer{NATSURL: deps.Cfg.NATSURL, Log: log, Handle: handlers.Dispatch}
	lanes, err := consumer.Run(deps.Ctx)
	if err != nil {
		deps.Log.Fatal("failed to start discord command consumer", zap.Error(err))
	}
	return lanes
}

func laneChecks(lanes commands.Lanes) []health.Check {
	return []health.Check{
		bus.LaneCheck("mod", lanes.Mod),
		bus.LaneCheck("default", lanes.Default),
	}
}
