package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/app/commands/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
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
	rpcURL := bus.RPCURL(natsURL)

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewCommands(client, pub, nil, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown

	nc, err := bus.Connect(rpcURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

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

	// Durable group subscription: exactly one instance handles the delete so
	// rows are not redundantly removed, and any instance failure is retried.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect group subscriber", zap.Error(err))
	}
	defer func() { _ = grouped.Close() }()

	if err := bus.Consume(ctx, nil, grouped, data.SubjectUserDeleted, func(msg *message.Message) error {

		var dto data.UserDeletedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("commands: bad user_deleted payload", zap.Error(err))
			return nil
		}

		if err := validate.UserID(dto.UserID); err != nil {
			log.Warn("commands: invalid user_id in user_deleted", zap.Error(err))
			return nil
		}

		if err := repo.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
			return err
		}

		log.Info("commands: deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}, log); err != nil {
		log.Fatal("failed to subscribe to user deleted events", zap.Error(err))
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")
	if err := rpc.SubscribeProjection(nc, repo, projectionSubject, "commands-rpc", log); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	commandsPrefix := env.Get("NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands")
	if err := rpc.SubscribeDashboard(nc, repo, commandsPrefix, "commands-rpc", log); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("commands service ready",
		zap.String("projection_subject", projectionSubject),
		zap.String("commands_prefix", commandsPrefix),
	)

	<-ctx.Done()

	log.Info("commands service shutting down")
}
