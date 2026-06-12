package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/projector/store"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/logger"

	"go.uber.org/zap"
)

const serviceName = "projector"

func main() {

	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	valkeyStore, err := store.NewValkey(
		env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		env.Get("VALKEY_PASSWORD", ""),
	)
	if err != nil {
		log.Fatal("failed to connect to valkey", zap.Error(err))
	}
	defer valkeyStore.Close()

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")

	// One durable group for the whole projector fleet: each event is folded
	// into Valkey exactly once, and the durable consumer keeps its position
	// across restarts.
	sub, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	defer func() { _ = sub.Close() }()

	projector := NewProjector(valkeyStore, log)

	if err := bus.Consume(ctx, nil, sub, data.SubjectUserChanged, projector.HandleUserChanged, log); err != nil {
		log.Fatal("failed to subscribe to user changes", zap.Error(err))
	}

	if err := bus.Consume(ctx, nil, sub, data.SubjectUserDeleted, projector.HandleUserDeleted, log); err != nil {
		log.Fatal("failed to subscribe to user deletions", zap.Error(err))
	}

	if err := bus.Consume(ctx, nil, sub, data.SubjectModuleChanged, projector.HandleModuleChanged, log); err != nil {
		log.Fatal("failed to subscribe to module changes", zap.Error(err))
	}

	// Ask the data services to replay their state so a fresh or wiped Valkey
	// converges to the full projection. Overwrites make this idempotent.
	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	if err := bus.PublishJSON(ctx, pub, data.SubjectReprojectRequest, struct{}{}); err != nil {
		log.Fatal("failed to request reprojection", zap.Error(err))
	}

	log.Info("projector ready")

	<-ctx.Done()

	log.Info("projector shutting down")
}
