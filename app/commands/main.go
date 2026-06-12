package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

const serviceName = "commands"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", "bagel_commands"),
	})
	if err != nil {
		log.Fatal("failed to open database", zap.Error(err))
	}

	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	if err := client.Schema.Create(ctx); err != nil {
		log.Fatal("failed to run migrations", zap.Error(err))
	}

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewCommands(client, pub, nil, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown

	// Broadcast subscription: every instance must drop its cached view when
	// any instance changes a command, so no queue group here.
	broadcast, err := bus.NewSubscriber(natsURL, "", log)
	if err != nil {
		log.Fatal("failed to connect broadcast subscriber", zap.Error(err))
	}
	defer func() { _ = broadcast.Close() }()

	if err := bus.Consume(ctx, nil, broadcast, data.SubjectCommandChanged, func(msg *message.Message) error {

		var dto data.CommandChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}

		repo.Invalidate(dto.UserID)
		return nil
	}, log); err != nil {
		log.Fatal("failed to subscribe to command changes", zap.Error(err))
	}

	log.Info("commands service ready")

	<-ctx.Done()

	log.Info("commands service shutting down")
}
