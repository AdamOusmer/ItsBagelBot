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
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/discord/ingress/internal/config"
	"ItsBagelBot/app/discord/ingress/internal/gateway"
	"ItsBagelBot/app/discord/ingress/internal/relay"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordrate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"go.uber.org/zap"
)

const serviceName = "discord-ingress"

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
		// No token means no gateway Identify is possible. Health still
		// serves so the pod stays Ready instead of crash-looping on a
		// deliberately unconfigured deploy.
		log.Info("DISCORD_BOT_TOKEN unset; discord ingress idle")
		health.Serve(cfg.ListenAddr, serviceName)
		<-ctx.Done()
		return
	}

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	// The interaction defer is the one REST call ingress makes; it still
	// pays the fleet-wide bucket outgress's calls pay, because Discord's
	// global limit is per bot token and ingress+outgress share one. See
	// internal/discordrate's package doc.
	rest := discordrate.NewLimitedClient(discordapi.NewClient(cfg.DiscordBotToken), discordrate.New(valkeyClient))

	pub, err := bus.NewPublisher(cfg.NATSURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	r := &relay.Relay{REST: rest, Pub: pub, Log: log}

	health.Serve(cfg.ListenAddr, serviceName)

	sess := gateway.Session{
		Token:  cfg.DiscordBotToken,
		Dial:   gateway.DialWS,
		Handle: r,
		Log:    log,
	}
	log.Info("discord ingress ready")
	if err := sess.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal("discord gateway stopped", zap.Error(err))
	}
	log.Info("discord ingress shutting down")
}
