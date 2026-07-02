package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ItsBagelBot/app/transactions/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/transactions/ent/runtime"
	"ItsBagelBot/app/transactions/repository"
	"ItsBagelBot/app/transactions/web"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

const serviceName = "transactions"

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

	if env.GetBool("DB_AUTO_MIGRATE", true) {
		if err := client.Schema.Create(ctx); err != nil {
			log.Fatal("failed to run migrations", zap.Error(err))
		}
	}

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewTransactions(client, pub)

	listenAddr := env.Get("LISTEN_ADDR", ":8080")
	httpApp := web.New(repo, web.Config{
		WebhookSecret: env.Get("TEBEX_WEBHOOK_SECRET", ""),
	}, log.Named("http"))

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpApp.Listen(listenAddr)
	}()

	log.Info("transactions service ready",
		zap.String("listen_addr", listenAddr),
		zap.Bool("tebex_webhook_configured", env.Get("TEBEX_WEBHOOK_SECRET", "") != ""),
	)

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		log.Fatal("transactions http server stopped", zap.Error(err))
	}

	log.Info("transactions service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpApp.ShutdownWithContext(shutdownCtx); err != nil {
		log.Warn("transactions http server shutdown failed", zap.Error(err))
	}
}
