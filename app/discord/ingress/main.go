// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Command ingress is the Discord half of the ingress/engine/outgress split
// (mirroring twitch-ingress -> sesame -> outgress). It holds the one Discord
// gateway Identify session for the fleet bot token and does nothing else:
// every event it receives is wrapped and published, never acted on. See
// internal/domain/discord's Event doc and internal/relay's package doc for
// why, and internal/relay/ack.go for the one exception (the inline
// interaction defer).
//
// Exactly one replica may run this: two Identify sessions on one bot token
// fight each other for the connection, same constraint app/dingress's
// ROLE=gateway had before this split.
package main

import (
	"ItsBagelBot/app/discord/ingress/internal/botstatus"
	"ItsBagelBot/app/discord/ingress/internal/config"
	"ItsBagelBot/app/discord/ingress/internal/gateway"
	"ItsBagelBot/app/discord/ingress/internal/presence"
	"ItsBagelBot/app/discord/ingress/internal/relay"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordrate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/svcboot"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const serviceName = "discord-ingress"

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log, ctx := core.Log, core.Ctx

	cfg := config.Load()
	if cfg.DiscordBotToken == "" {
		// No token means no gateway Identify is possible. Health still
		// serves so the pod stays Ready instead of crash-looping on a
		// deliberately unconfigured deploy.
		log.Info("DISCORD_BOT_TOKEN unset; discord ingress idle")
		health.Serve(cfg.ListenAddr, serviceName)
		core.Await()
		return
	}

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()

	// The interaction defer is the one REST call ingress makes; it still
	// pays the fleet-wide bucket outgress's calls pay, because Discord's
	// global limit is per bot token and ingress+outgress share one. See
	// internal/discordrate's package doc.
	rest := discordrate.NewLimitedClient(discordapi.NewClient(cfg.DiscordBotToken), discordrate.New(valkeyClient))

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	svcboot.FatalIf(log, err, "failed to connect publisher")
	defer func() { _ = pub.Close() }()

	r := &relay.Relay{REST: rest, Pub: pub, Log: log}

	// RPC connection, separate from pub above: pub is the fire-and-forget
	// event publisher (no reply subject), while the counts lookup behind
	// gateway presence is a request/reply call. See presence.NewFetch's doc.
	rpcConn := svcboot.MustRPCConn(core, bus.RPCURL(cfg.NATSRPCURL))
	defer rpcConn.Close()

	// HOSTNAME is the pod name the kubelet injects; it is the only field of
	// the status key that says WHICH ingress wrote it, which matters the one
	// time two replicas exist by accident (two Identify sessions on one bot
	// token fight, see this file's package doc).
	pod := env.Get("HOSTNAME", "")
	status := botstatus.New(valkeyClient, pod, log)
	go status.Run(ctx)

	health.ServeSet(cfg.ListenAddr, healthSet(core, rpcConn, status))

	sess := gateway.Session{
		Token:  cfg.DiscordBotToken,
		Dial:   gateway.DialWS,
		Handle: r,
		Log:    log,
		Status: status,
		// The connect window survives a restart deliberately: the loop that
		// got this bot's token reset was a crash-loop, and a per-process
		// count of 800 is 800 per crash (see gateway/budget.go).
		Connects: botstatus.NewConnectLog(valkeyClient, pod),
		Presence: &presence.Source{
			Fetch: presence.NewFetch(rpcConn, cfg.UsersCountsSubject),
			Log:   log,
		},
		PresenceInterval: presence.RefreshInterval,
	}
	log.Info("discord ingress ready")
	if err := sess.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal("discord gateway stopped", zap.Error(err))
	}
	log.Info("discord ingress shutting down")
}

// healthSet is ingress's own report plus the RPC responder that serves it, the
// one engine folds into health.itsbagelbot.com/discord -- this process is not
// routed from outside, so that RPC is the only way its verdict is visible.
//
// There is no lane check here because ingress consumes nothing: it publishes
// every event it receives and never binds a durable. What is left is the RPC
// connection and the responder registration on it. Until this Set existed the
// process served zero checks, which meant a pod that had lost NATS entirely
// still answered /status 200 and stayed Ready with the whole gateway feed
// going nowhere.
//
// The idle path above (no DISCORD_BOT_TOKEN) deliberately does not come
// through here: it has no NATS connection to register a responder on, and
// staying Ready with nothing to check is the behaviour it is there for.
func healthSet(core svcboot.Core, nc *nats.Conn, status *botstatus.Reporter) *health.Set {
	set := svcboot.NewHealthSet(svcboot.Health{
		Log: core.Log, NC: nc, Service: serviceName, QueueGroup: serviceName + "-rpc",
	}, status.ReadyCheck())
	// The gateway is the one dependency whose failure a restart can actually
	// fix, so it is also the one liveness gate in the fleet: a fatal close
	// code means this process will never hold the bot's Identify session
	// again, and a stalled heartbeat means the loop that owns it is gone.
	set.Live(status.LiveCheck())
	return set
}
