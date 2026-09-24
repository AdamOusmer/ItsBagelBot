// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"time"

	"ItsBagelBot/app/discord/ingress/internal/botstatus"
	"ItsBagelBot/app/discord/ingress/internal/config"
	"ItsBagelBot/app/discord/ingress/internal/gateway"
	"ItsBagelBot/app/discord/ingress/internal/presence"
	"ItsBagelBot/app/discord/ingress/internal/relay"
	"ItsBagelBot/internal/discordboot"
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
	idle := discordboot.Service{Name: serviceName, Token: cfg.DiscordBotToken, Listen: cfg.ListenAddr}
	if discordboot.IdleIfNoToken(core, idle) {
		return
	}

	valkeyClient := svcboot.MustValkey(core)
	defer valkeyClient.Close()

	rest := discordrate.NewClient(cfg.DiscordBotToken, discordrate.New(valkeyClient))

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	svcboot.FatalIf(log, err, "failed to connect publisher")
	defer func() { _ = pub.Close() }()

	r := &relay.Relay{REST: rest, Pub: pub, Log: log}

	rpcConn := svcboot.MustRPCConn(core, bus.RPCURL(cfg.NATSRPCURL))
	defer rpcConn.Close()

	pod := env.Get("HOSTNAME", "")
	status := botstatus.New(valkeyClient, pod, log)
	go status.Run(ctx)

	health.ServeSet(cfg.ListenAddr, healthSet(core, rpcConn, status))

	gwLog := log.With(zap.String("pod", pod), zap.Int64("boot_id", time.Now().UnixMilli()))

	sess := gateway.Session{
		Token:    cfg.DiscordBotToken,
		Dial:     gateway.DialWS,
		Handle:   r,
		Log:      gwLog,
		Status:   status,
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

func healthSet(core svcboot.Core, nc *nats.Conn, status *botstatus.Reporter) *health.Set {
	set := svcboot.NewHealthSet(svcboot.Health{
		Log: core.Log, NC: nc, Service: serviceName, QueueGroup: serviceName + "-rpc",
	}, status.ReadyCheck())
	set.Live(status.LiveCheck())
	return set
}
