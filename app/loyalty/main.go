// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
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
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/logger"
	"ItsBagelBot/pkg/monitor"

	"github.com/AdamOusmer/recipes/runtime"
	"github.com/AdamOusmer/recipes/svc/zvngr"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const serviceName = "loyalty"

// registerConsumers wires the event subscriptions onto repo. Everything here
// is delta folding or cleanup that must happen exactly once per event, so all
// subjects ride the grouped (queue) subscriber.
func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Loyalty, grouped bus.Subscriber, k *zvngr.K, log *zap.Logger) error {
	subs := []struct {
		name    string
		subject string
		handle  func(*bus.Message) error
	}{
		{"loyalty earned events", k.ZHZPS().Subject, recordEarned(repo, log)},
		{"loyalty counter events", k.ZEOFI().Subject, recordBumps(repo, log)},
		{"user deleted events", k.ZBPUZ().Subject, deleteAllForUser(repo, log)},
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

// fatalIf aborts startup on err: loyalty cannot run degraded without any of
// its core dependencies, so a failed step must crash the pod for Kubernetes
// to restart it.
func fatalIf(log *zap.Logger, err error, msg string) {
	if err != nil {
		log.Fatal(msg, zap.Error(err))
	}
}

func main() {
	log := logger.New(env.Get("APP_ENV", "development")).Named(serviceName)
	defer func() { _ = log.Sync() }()

	nrApp, err := monitor.New(serviceName, log)
	fatalIf(log, err, "failed to start new relic")
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
	fatalIf(log, err, "failed to open database")

	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	if env.GetBool("DB_AUTO_MIGRATE", true) {
		fatalIf(log, client.Schema.Create(ctx), "failed to run migrations")
	}

	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	rpcURL := bus.RPCURL(natsURL)

	repo := repository.NewLoyalty(client, driver, nrApp, log)
	defer repo.Close(context.Background()) // flushes pending deltas on shutdown

	nc, err := bus.Connect(rpcURL, serviceName)
	fatalIf(log, err, "failed to connect to nats")
	defer nc.Close()

	busConn, k, grantsDenied := connectRecipes(ctx, natsURL, nc, log)
	defer busConn.Close()

	// Durable group subscription: exactly one instance folds each delta event,
	// and an instance failure is retried on another.
	grouped, err := bus.NewSubscriber(natsURL, serviceName, log)
	fatalIf(log, err, "failed to connect group subscriber")
	defer func() { _ = grouped.Close() }()

	fatalIf(log, registerConsumers(ctx, nrApp, repo, grouped, k, log), "failed to subscribe to events")

	loyaltyPrefix := strings.TrimSuffix(k.ZFM5N(), ".>")
	fatalIf(log, rpc.Subscribe(nc, repo, loyaltyPrefix, "loyalty-rpc", nrApp, log), "failed to subscribe loyalty rpc")
	fatalIf(log, bus.SubscribeRPCHealth(nc, serviceName, "loyalty-rpc"), "failed to subscribe rpc health")

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), serviceName,
		health.Bool("nats", nc.IsConnected),
		health.Bool("nats_grants", func() bool { return !grantsDenied.Load() }),
	)

	log.Info("loyalty service ready",
		zap.String("loyalty_prefix", loyaltyPrefix),
	)

	<-ctx.Done()

	log.Info("loyalty service shutting down")
}

// connectRecipes dials the BUS-plane connection, binds loyalty's recipes
// binding over it and nc, wires its permission-violation Watchdog, and
// preflights the JetStream grants it declares. The returned pointer backs
// the nats_grants health check.
func connectRecipes(ctx context.Context, natsURL string, nc *nats.Conn, log *zap.Logger) (*nats.Conn, *zvngr.K, *atomic.Bool) {
	busConn, err := bus.ConnectBus(natsURL, serviceName)
	if err != nil {
		log.Fatal("failed to connect bus-plane nats", zap.Error(err))
	}

	k, err := zvngr.Up(zvngr.U{Bus: busConn, Rpc: nc})
	if err != nil {
		log.Fatal("failed to bind loyalty's recipes binding", zap.Error(err))
	}

	// grantsDenied flips true the first time the NATS server reports loyalty's
	// BUS account denied a permission its manifest declares (see
	// runtime.Watchdog).
	grantsDenied := &atomic.Bool{}
	watchdog := runtime.NewWatchdog(k.M(), func(subject, canonical string, err error) {
		log.Error("nats permission violation",
			zap.String("subject", subject), zap.String("canonical", canonical), zap.Error(err))
	}, func() { grantsDenied.Store(true) })
	bus.GuardConnection(busConn, watchdog.Handler())

	if err := runtime.PreflightStreams(ctx, busConn, k.Expectations(), 0); err != nil {
		log.Fatal("recipes preflight failed: missing jetstream grant(s)", zap.Error(err))
	}

	return busConn, k, grantsDenied
}
