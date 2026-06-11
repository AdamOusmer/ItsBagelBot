package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

const serviceName = "users"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	keysetJSON, err := os.ReadFile(env.MustGet("TINK_KEYSET_PATH"))
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}

	packer, err := crypto.NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}

	driver, err := db.NewDriver(db.Config{
		Address:  env.Get("DB_ADDR", "127.0.0.1:3306"),
		Username: env.MustGet("DB_USER"),
		Password: env.MustGet("DB_PASS"),
		Schema:   env.Get("DB_SCHEMA", "bagel_users"),
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

	repo := repository.NewUsers(client, packer, pub)
	defer repo.Close()

	// Broadcast subscription: every instance must drop its cached view when
	// any instance changes a user, so no queue group here.
	broadcast, err := bus.NewSubscriber(natsURL, "", log)
	if err != nil {
		log.Fatal("failed to connect broadcast subscriber", zap.Error(err))
	}
	defer func() { _ = broadcast.Close() }()

	if err := bus.Consume(ctx, broadcast, data.SubjectUserChanged, func(msg *message.Message) error {

		var dto data.UserChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}

		repo.Invalidate(dto.UserID)
		return nil
	}, log); err != nil {
		log.Fatal("failed to subscribe to user changes", zap.Error(err))
	}

	// Durable group subscription: exactly one instance answers a reproject
	// request by replaying the table as change events.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect group subscriber", zap.Error(err))
	}
	defer func() { _ = grouped.Close() }()

	if err := bus.Consume(ctx, grouped, data.SubjectReprojectRequest, func(*message.Message) error {
		return repo.Reproject(ctx)
	}, log); err != nil {
		log.Fatal("failed to subscribe to reproject requests", zap.Error(err))
	}

	log.Info("users service ready")

	<-ctx.Done()

	log.Info("users service shutting down")
}
