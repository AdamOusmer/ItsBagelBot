package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"ItsBagelBot/app/projector/rpc"
	"ItsBagelBot/app/projector/store"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"

	"github.com/nats-io/nats.go"
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

	if err := bus.EnsureStreams(ctx, natsURL, bus.DataStreams, log); err != nil {
		log.Fatal("failed to provision jetstream streams", zap.Error(err))
	}

	// One durable group for the whole projector fleet: each event is folded
	// into Valkey exactly once, and the durable consumer keeps its position
	// across restarts.
	sub, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect subscriber", zap.Error(err))
	}
	defer func() { _ = sub.Close() }()

	nc, err := bus.Connect(natsURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect nats", zap.Error(err))
	}
	defer nc.Close()

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

	// Stream Online Pre-Warming
	streamTopic := env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream")
	usersTopic := env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get")
	modulesTopic := env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get")
	commandsTopic := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")

	if _, err := nc.Subscribe(streamTopic, func(msg *nats.Msg) {
		projector.HandleStreamOnline(msg, nc, usersTopic, modulesTopic, commandsTopic)
	}); err != nil {
		log.Fatal("failed to subscribe to stream online events", zap.Error(err))
	}

	subject := env.Get("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get")
	if err := rpc.SubscribeStatus(nc, valkeyStore, subject, usersTopic, "projector-rpc", log); err != nil {
		log.Fatal("failed to subscribe status rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("projector ready",
		zap.String("status_subject", subject),
		zap.String("stream_subject", streamTopic))

	<-ctx.Done()

	log.Info("projector shutting down")
}
