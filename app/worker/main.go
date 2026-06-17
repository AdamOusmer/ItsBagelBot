package main

import (
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const serviceName = "worker"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	nc, err := bus.Connect(natsURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	sub, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	defer func() { _ = sub.Close() }()

	// Optional: Initialize New Relic for distributed tracing inside ConsumeWeighted
	var nrApp *newrelic.Application
	if nrKey := env.Get("NEW_RELIC_LICENSE_KEY", ""); nrKey != "" {
		nrApp, err = newrelic.NewApplication(
			newrelic.ConfigAppName(serviceName),
			newrelic.ConfigLicense(nrKey),
		)
		if err != nil {
			log.Warn("failed to initialize new relic", zap.Error(err))
		}
	}

	err = bus.ConsumeWeighted(
		ctx,
		nrApp,
		sub,
		ingressSubject,
		handleIngress(log, pub),
		concurrencyLimit,
		log,
	)
	if err != nil {
		log.Fatal("failed to start jetstream consumption", zap.Error(err))
	}

	log.Info("worker is now listening for ingress events", zap.String("subject", ingressSubject))

	// Health server blocks the main thread while goroutines process in the background
	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)
}
