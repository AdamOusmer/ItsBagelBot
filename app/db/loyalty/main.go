// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/db/loyalty/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	"ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/app/db/loyalty/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
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

// Grouped subscriber only: every event must be folded exactly once.
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

func deleteAllForUser(repo *repository.Loyalty, log *zap.Logger) func(*bus.Message) error {
	return consumers.OnUserDeleted(serviceName, log, repo.DeleteAllForUser)
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, "bagel_loyalty")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	repo := repository.NewLoyalty(client, driver, core.NR, log)
	defer repo.Close(context.Background())

	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()

	grouped, err := bus.NewSubscriber(core.NATSURL, serviceName, log)
	svcboot.FatalIf(log, err, "failed to connect group subscriber")
	defer func() { _ = grouped.Close() }()

	svcboot.FatalIf(log, registerConsumers(core.Ctx, core.NR, repo, grouped, log), "failed to subscribe to events")

	loyaltyPrefix := env.Get("NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty")
	svcboot.FatalIf(log, rpc.Subscribe(rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
	}, loyaltyPrefix),
		"failed to subscribe loyalty rpc")
	// The lane check must stay hard: a wedged consumer stops points accruing while health reads green.
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
