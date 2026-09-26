// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/db/loyalty/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/loyalty/ent/runtime"
	"ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/app/db/loyalty/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
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
		{"user lifecycle events", data.SubjectUserChanged, restoreUser(repo, log)},
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

type bumpApplier interface {
	ApplyBumps(context.Context, data.CounterBumpedDTO) error
}

func recordBumps(repo bumpApplier, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.CounterBumpedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad counter payload", zap.Error(err))
			return nil
		}
		if dto.BatchID == "" {
			dto.BatchID = msg.UUID
		}
		err := repo.ApplyBumps(msg.Context(), dto)
		if errors.Is(err, repository.ErrInvalidInput) {
			log.Warn("loyalty: dropping invalid counter batch",
				zap.String("batch_id", dto.BatchID),
				zap.Uint64("user_id", dto.UserID),
				zap.Error(err))
			return nil
		}
		return err
	}
}

func deleteAllForUser(repo *repository.Loyalty, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad deletion payload", zap.Error(err))
			return nil
		}
		return repo.DeleteAccount(msg.Context(), dto.UserID, dto.AccountCreatedAt)
	}
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
	svcboot.FatalIf(log, repo.EnsureWatchSchema(core.Ctx), "failed to initialize watch inbox")
	vc := svcboot.MustValkey(core)
	defer vc.Close()
	watchCtx, stopWatch := context.WithCancel(core.Ctx)
	watchDone := make(chan struct{})
	watchConsumer := watchtime.NewConsumer(vc, repo.ApplyWatchAward, log, watchtime.WithHistoryMaintenance(repo.PruneWatchHistory))
	svcboot.FatalIf(log, watchConsumer.Ensure(core.Ctx), "failed to initialize watch outbox consumer")
	go func() { defer close(watchDone); watchConsumer.Run(watchCtx) }()
	defer func() { stopWatch(); <-watchDone }()

	stopPruner := repo.StartBatchReceiptPruner(core.Ctx, repository.BatchReceiptPruneInterval, repository.BatchReceiptRetention)
	defer stopPruner()

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
	}, bus.LaneCheck("data", grouped), health.Check{Name: "watchtime", Probe: watchConsumer.Check})

	log.Info("loyalty service ready",
		zap.String("loyalty_prefix", loyaltyPrefix),
	)

	core.Await()
}

func restoreUser(repo *repository.Loyalty, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserChangedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad lifecycle payload", zap.Error(err))
			return nil
		}
		return repo.RestoreUser(msg.Context(), dto.UserID, dto.AccountCreatedAt)
	}
}
