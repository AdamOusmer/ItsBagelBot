package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/transactions/ent"
	"ItsBagelBot/app/transactions/repository"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"

	"go.uber.org/zap"
)

const serviceName = "transactions"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", "bagel_transactions"),
	})
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}

	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	if err := client.Schema.Create(ctx); err != nil {
		log.Fatal("failed to run migrations", zap.Error(err))
	}

	pub, err := bus.NewPublisher(env.Get("NATS_URL", "nats://127.0.0.1:4222"), log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	_ = repository.NewTransactions(client, pub) // wired to the ingress (Tebex webhook consumer) next

	log.Info("transactions service ready")

	<-ctx.Done()

	log.Info("transactions service shutting down")
}
