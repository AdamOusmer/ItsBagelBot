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
	"ItsBagelBot/app/dingress/internal/gateway"
	"ItsBagelBot/app/dingress/internal/store"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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
		log.Info("DISCORD_BOT_TOKEN unset; dingress idle")
		health.Serve(cfg.ListenAddr, serviceName)
		<-ctx.Done()
		return
	}

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	bot := &community.Bot{
		REST:    discordapi.NewClient(cfg.DiscordBotToken),
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
