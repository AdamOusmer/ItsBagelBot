// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"
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
	"ItsBagelBot/pkg/codec"
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
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.LoyaltyEarnedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
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
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.CounterBumpedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
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
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
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

// healthSet builds the checks loyalty answers with on both /status and its
// health RPC. One Set backs both surfaces, so the aggregate the projector
// serves at /db can never disagree with what this pod reports over HTTP at the
// same instant.
//
// mysql check alongside nats: PingContext exercises the same pool repository
// code uses, catching a wedged pool or rotated-out creds that nc.IsConnected
// alone would miss (pkg/db/health.go). Degrades rather than fails readiness: a
// hard-fail would pull every loyalty pod out of service on the same DB blip
// simultaneously, turning a brief outage into a total one. A healthy ping lands
// in single-digit ms (measured ~3.6ms pod-to-MySQL RTT); much higher means the
// pool went cold and is paying the ~18ms handshake instead of reusing a conn.
// It stays here rather than being hoisted into the projector's /db aggregate:
// each data service reports its own database because the schemas are expected
// to split across servers, and one hoisted check could not name which one went.
//
// The lane check covers the durable group folding data.loyalty.earned,
// data.loyalty.counters and data.users.deleted. Its verdict is hard, not
// degrading: a consumer that stays bound while failing to fetch stops points
// accruing entirely, with NATS and MySQL both still reading green.
func healthSet(nc *nats.Conn, pool *sql.DB, grouped bus.Subscriber) *health.Set {
	return health.NewSet(serviceName,
		health.NATS("nats", nc),
		bus.LaneCheck("data", grouped),
		health.Degrades(db.HealthCheck("mysql", pool)))
}

// serveHealth builds the Set, registers the responder that answers out of it,
// and starts the HTTP surface. It is a function rather than four lines in main
// because the ordering is load-bearing -- the responder can only be registered
// against a Set that already exists, and the check watching that registration
// only exists afterwards -- and main is already carrying every other fatal
// branch of the boot.
func serveHealth(nc *nats.Conn, pool *sql.DB, grouped bus.Subscriber, log *zap.Logger) {
	set := healthSet(nc, pool, grouped)
	rpcHealth, err := bus.SubscribeRPCHealth(nc, serviceName, "loyalty-rpc", set)
	if err != nil {
		log.Fatal("failed to subscribe rpc health", zap.Error(err))
	}
	set.Add(rpcHealth)

	health.ServeSet(env.Get("LISTEN_ADDR", ":8080"), set)
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
	serveHealth(nc, driver.DB(), grouped, log)

	log.Info("loyalty service ready",
		zap.String("loyalty_prefix", loyaltyPrefix),
	)

	<-ctx.Done()

	log.Info("loyalty service shutting down")
}
