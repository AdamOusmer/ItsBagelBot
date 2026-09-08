// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/db/loyalty/ent"
	// Wire the ent schema runtime (field defaults like updated_at, and the name
	// normalization hook). Without this blank import the generated descriptors
	// stay uninitialized and every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	"ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/app/db/loyalty/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName = "loyalty"
	queueGroup  = "loyalty-rpc"
)

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

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(log, "bagel_loyalty")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	repo := repository.NewLoyalty(client, driver, core.NR, log)
	defer repo.Close(context.Background()) // flushes pending deltas on shutdown

	// Only the RPC connection and the durable group, not the full MustNATS set:
	// loyalty publishes nothing and needs no broadcast subscriber -- every
	// subject it reads is a delta that exactly one instance must fold.
	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()

	grouped, err := bus.NewSubscriber(core.NATSURL, serviceName, log)
	svcboot.FatalIf(log, err, "failed to connect group subscriber")
	defer func() { _ = grouped.Close() }()

	svcboot.FatalIf(log, registerConsumers(core.Ctx, core.NR, repo, grouped, log), "failed to subscribe to events")

	loyaltyPrefix := env.Get("NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty")
	svcboot.FatalIf(log, rpc.Subscribe(nc, repo, loyaltyPrefix, queueGroup, core.NR, log),
		"failed to subscribe loyalty rpc")
	// The lane check covers the durable group folding data.loyalty.earned,
	// data.loyalty.counters and data.users.deleted. Its verdict is hard, not
	// degrading: a consumer that stays bound while failing to fetch stops points
	// accruing entirely, with NATS and MySQL both still reading green.
	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", grouped))

	log.Info("loyalty service ready",
		zap.String("loyalty_prefix", loyaltyPrefix),
	)

	core.Await()
}
