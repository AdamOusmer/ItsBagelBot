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

// main reads as a sequence of named boot phases; each phase logs and exits
// the process on its own fatal error (matching what this used to do inline)
// so the phase order below is also the exact fatal-error order.
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

	rest := discordrate.NewLimitedClient(discordapi.NewClient(cfg.DiscordBotToken), discordrate.New(valkeyClient))
	liveStore := kv.New(valkeyClient)
	reauth := kv.NewReauthStore(valkeyClient)
	botStatus := kv.NewBotStatusReader(valkeyClient)
	lockdowns := kv.NewLockdownStore(valkeyClient)

	applicationID := registerSlashCommands(ctx, rest, log)

	nc := svcboot.MustRPCConn(core, cfg.NATSRPCURL)
	defer nc.Close()

	// discord-data-backed, same as engine's: the dashboard's setup, unbind
	// and settings writes must land in MySQL, not in a Valkey key the engine
	// no longer treats as the truth. Unconditional -- see the note on
	// discordstore.DefaultRPCPrefix for why the Valkey-only mode is gone.
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

// ensureOutgressStream provisions DISCORD_OUTGRESS, the stream this process
// consumes (see pkg/bus.DiscordOutgressStream's doc); it never provisions
// DISCORD_INGRESS, which it only ever reads nothing from at all -- that is
// engine's job, as the consumer on that side.
func ensureOutgressStream(ctx context.Context, cfg config.Config, log *zap.Logger) {
	svcboot.FatalIf(log, bus.EnsureStreams(ctx, cfg.NATSURL, []bus.StreamSpec{bus.DiscordOutgressStream}, log),
		"failed to provision the DISCORD_OUTGRESS stream")
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

// rpcDeps is subscribeRPCs's whole input, collapsed from eight positional
// parameters into one struct (CodeScene: Excess Number of Function
// Arguments, over its 4-parameter limit).
type rpcDeps struct {
	NC            *nats.Conn
	Cfg           config.Config
	Rest          *discordrate.LimitedClient
	Store         discordstore.Store
	ApplicationID string
	LiveStore     kv.LiveStore
	Reauth        kv.ReauthStore
	BotStatus     kv.BotStatusReader
	NRApp         *newrelic.Application
	Log           *zap.Logger
}

// subscribeRPCs wires the dashboard-facing guild setup RPC and the
// engine-facing channel-management/live RPC onto the same connection.
func subscribeRPCs(deps rpcDeps) {
	setupWorker := setup.New(setup.Config{Discord: deps.Rest, Store: deps.Store, Log: deps.Log.Named("setup")})
	if err := rpc.SubscribeSetup(setupWorker, rpc.SetupWiring{
		NC: deps.NC, Prefix: deps.Cfg.RPCPrefix, Queue: deps.Cfg.RPCQueue, App: deps.NRApp,
		Reauth: deps.Reauth, Status: deps.BotStatus, Log: deps.Log.Named("rpc"),
	}); err != nil {
		deps.Log.Fatal("failed to subscribe discord guild setup rpc", zap.Error(err))
	}
	engineWiring := rpc.EngineWiring{
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
	Lockdowns     kv.LockdownStore
	Log           *zap.Logger
}

// startCommandConsumer returns the bound lanes, not just their close func:
// healthSet reports on each of them by name.
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

// laneChecks is outgress's own report, the one engine folds into
// health.itsbagelbot.com/discord -- outgress is not routed from outside, so the
// health RPC is the only way its verdict reaches the vertical's answer.
//
// The two lane checks are the reason this is a Set and not a lone NATS check.
// Draining DISCORD_OUTGRESS is the entire job of this process, and a durable
// that is still bound but no longer fetching leaves the connection check green
// while every Command silently ages out at the stream's 60s MaxAge. Checked per
// lane so the report names which of mod/default wedged, since mod-first
// priority means the two fail independently.
func laneChecks(lanes commands.Lanes) []health.Check {
	return []health.Check{
		bus.LaneCheck("mod", lanes.Mod),
		bus.LaneCheck("default", lanes.Default),
	}
}
