package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/commands/ent"
	// Wire the ent schema runtime (field defaults like updated_at, and the name
	// normalization hook). Without this blank import the generated descriptors
	// stay uninitialized and every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/commands/ent/runtime"
	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/app/commands/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/ThreeDotsLabs/watermill/message"

	"go.uber.org/zap"
)

const serviceName = "commands"

// registerConsumers wires the event subscriptions onto repo: cache
// invalidation fans out to every instance (broadcast), while use-counter and
// account-deletion events are handled once per event (grouped).
func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Commands, broadcast, grouped message.Subscriber, log *zap.Logger) error {
	// Use-counter events from the worker: exactly one instance sums each event
	// (queue group), the repo batches them and flushes uses = uses + n.
	subs := []struct {
		name    string
		sub     message.Subscriber
		subject string
		handle  func(*message.Message) error
	}{
		{"command changes", broadcast, data.SubjectCommandChanged, invalidateOnChange(repo)},
		{"command used events", grouped, data.SubjectCommandUsed, recordUse(repo, log)},
		{"user deleted events", grouped, data.SubjectUserDeleted, deleteAllForUser(repo, log)},
	}
	for _, s := range subs {
		if err := bus.Consume(ctx, nrApp, s.sub, s.subject, s.handle, log); err != nil {
			return fmt.Errorf("subscribe to %s: %w", s.name, err)
		}
	}
	return nil
}

// invalidateOnChange drops the cached view of the changed user.
func invalidateOnChange(repo *repository.Commands) func(*message.Message) error {
	return func(msg *message.Message) error {
		var dto data.CommandChangedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}
		repo.Invalidate(dto.UserID)
		return nil
	}
}

// recordUse folds a worker use-counter event into the repo's accumulator. A
// malformed payload is dropped (nil), not retried.
func recordUse(repo *repository.Commands, log *zap.Logger) func(*message.Message) error {
	return func(msg *message.Message) error {
		var dto data.CommandUsedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("commands: bad command_used payload", zap.Error(err))
			return nil
		}
		repo.RecordUse(dto.UserID, dto.Name, dto.Count)
		return nil
	}
}

// deleteAllForUser removes every command of a deleted account. Malformed or
// invalid payloads are dropped; a DB failure is returned for retry.
func deleteAllForUser(repo *repository.Commands, log *zap.Logger) func(*message.Message) error {
	return func(msg *message.Message) error {
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
	}
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

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
		Schema:   env.Get("DB_SCHEMA", "bagel_commands"),
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
	rpcURL := bus.RPCURL(natsURL)

	pub, err := bus.NewPublisher(natsURL, log)
	if err != nil {
		log.Fatal("failed to connect publisher", zap.Error(err))
	}
	defer func() { _ = pub.Close() }()

	repo := repository.NewCommands(client, pub, nrApp, log)
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

	// Durable group subscription: exactly one instance handles the delete so
	// rows are not redundantly removed, and any instance failure is retried.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect group subscriber", zap.Error(err))
	}
	defer func() { _ = grouped.Close() }()

	if err := registerConsumers(ctx, nrApp, repo, broadcast, grouped, log); err != nil {
		log.Fatal("failed to subscribe to events", zap.Error(err))
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")
	if err := rpc.SubscribeProjection(nc, repo, projectionSubject, "commands-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	commandsPrefix := env.Get("NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands")
	if err := rpc.SubscribeDashboard(nc, repo, commandsPrefix, "commands-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, "commands-rpc"); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("commands service ready",
		zap.String("projection_subject", projectionSubject),
		zap.String("commands_prefix", commandsPrefix),
	)

	<-ctx.Done()

	log.Info("commands service shutting down")
}
