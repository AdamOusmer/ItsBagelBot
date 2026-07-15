package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/loyalty/ent"
	// Wire the ent schema runtime (field defaults like updated_at, and the name
	// normalization hook). Without this blank import the generated descriptors
	// stay uninitialized and every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/loyalty/ent/runtime"
	"ItsBagelBot/app/loyalty/repository"
	"ItsBagelBot/app/loyalty/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

const serviceName = "loyalty"

// registerConsumers wires the event subscriptions onto repo. Everything here
// is delta folding or cleanup that must happen exactly once per event, so all
// subjects ride the grouped (queue) subscriber.
func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Loyalty, grouped bus.Subscriber, log *zap.Logger) error {
	subs := []struct {
		name    string
		subject string
		handle  func(*bus.Message) error
	}{
		{"loyalty earned events", data.SubjectLoyaltyEarned, recordEarned(repo, log)},
		{"loyalty counter events", data.SubjectLoyaltyCounters, recordBumps(repo, log)},
		{"user deleted events", data.SubjectUserDeleted, deleteAllForUser(repo, log)},
	}
	for _, s := range subs {
		if err := bus.Consume(ctx, nrApp, grouped, s.subject, s.handle, log); err != nil {
			return fmt.Errorf("subscribe to %s: %w", s.name, err)
		}
	}
	return nil
}

// recordEarned folds a worker earned event into the repo's accumulator. A
// malformed payload is dropped (nil), not retried.
func recordEarned(repo *repository.Loyalty, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.LoyaltyEarnedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad earned payload", zap.Error(err))
			return nil
		}
		repo.RecordEarned(dto)
		return nil
	}
}

// recordBumps folds a worker counter event into the repo's accumulator.
func recordBumps(repo *repository.Loyalty, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.CounterBumpedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad counter payload", zap.Error(err))
			return nil
		}
		repo.RecordBumps(dto)
		return nil
	}
}

// deleteAllForUser removes every loyalty row of a deleted account. Malformed
// or invalid payloads are dropped; a DB failure is returned for retry.
func deleteAllForUser(repo *repository.Loyalty, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserDeletedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad user_deleted payload", zap.Error(err))
			return nil
		}
		if err := validate.UserID(dto.UserID); err != nil {
			log.Warn("loyalty: invalid user_id in user_deleted", zap.Error(err))
			return nil
		}
		if err := repo.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
			return err
		}
		log.Info("loyalty: deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}
}

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
		Schema:   env.Get("DB_SCHEMA", "bagel_loyalty"),
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

	repo := repository.NewLoyalty(client, driver, nrApp, log)
	defer repo.Close(context.Background()) // flushes pending deltas on shutdown

	nc, err := bus.Connect(rpcURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	// Durable group subscription: exactly one instance folds each delta event,
	// and an instance failure is retried on another.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	if err != nil {
		log.Fatal("failed to connect group subscriber", zap.Error(err))
	}
	defer func() { _ = grouped.Close() }()

	if err := registerConsumers(ctx, nrApp, repo, grouped, log); err != nil {
		log.Fatal("failed to subscribe to events", zap.Error(err))
	}

	loyaltyPrefix := env.Get("NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty")
	if err := rpc.Subscribe(nc, repo, loyaltyPrefix, "loyalty-rpc", nrApp, log); err != nil {
		log.Fatal("failed to subscribe loyalty rpc", zap.Error(err))
	}
	if err := bus.SubscribeRPCHealth(nc, serviceName, "loyalty-rpc"); err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), nc.IsConnected)

	log.Info("loyalty service ready",
		zap.String("loyalty_prefix", loyaltyPrefix),
	)

	<-ctx.Done()

	log.Info("loyalty service shutting down")
}
