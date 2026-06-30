package main

import (
	"ItsBagelBot/app/plain/internal/config"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"
	pkg_valkey "ItsBagelBot/pkg/valkey"
	"time"

	"go.uber.org/zap"
)

const serviceName = "outgress"

const (
	nakDelay        = 1 * time.Second
	maxRedeliveries = 2
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

	cfg := config.Load()

	valkeyClient, err := pkg_valkey.NewClient(cfg.ValkeyAddr, cfg.ValkeyPassword)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

}
