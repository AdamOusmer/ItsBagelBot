// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName    = "loyalty"
	queueGroup     = "loyalty-rpc"
	counterWorkers = 32
)

// terminationGrace must match terminationGracePeriodSeconds in deploy/k8s/loyalty.yaml.
const (
	terminationGrace = 45 * time.Second
	preStopDrain     = 10 * time.Second
	exitReserve      = 5 * time.Second
	shutdownBudget   = terminationGrace - preStopDrain - exitReserve
)

func shutdownContext(signal context.Context, budget time.Duration) context.Context {
	ctx, cancel := context.WithCancelCause(context.Background())
	context.AfterFunc(signal, func() {
		time.AfterFunc(budget, func() { cancel(errShutdownBudget) })
	})
	return ctx
}

var errShutdownBudget = errors.New("shutdown budget exhausted")

type closer interface{ Close(context.Context) error }

type counterIntake struct {
	grouped  bus.Subscriber
	consumer *bus.ConcurrentConsumer
	counters closer
}

func (in counterIntake) stop(ctx context.Context, log *zap.Logger) {
	if err := closeWithin(ctx, in.grouped.Close); err != nil {
		log.Warn("loyalty: group subscriber shutdown failed", zap.Error(err))
	}
	if err := in.consumer.Drain(ctx); err != nil {
		log.Warn("loyalty: counter consumer shutdown failed", zap.Error(err))
	}
	if err := in.counters.Close(ctx); err != nil {
		log.Warn("loyalty: counter processor shutdown failed", zap.Error(err))
	}
}

func closeWithin(ctx context.Context, closeFn func() error) error {
	done := make(chan error, 1)
	go func() { done <- closeFn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// Grouped subscriber keeps account cleanup and delta folding on one durable lane.
// Counter handlers share bounded batches and wait for persistence before ACK.
func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Loyalty, counters counterProcessor, grouped bus.Subscriber, log *zap.Logger) (*bus.ConcurrentConsumer, error) {
	subs := []struct {
		name    string
		subject string
		handle  func(*bus.Message) error
	}{
		{"loyalty earned events", data.SubjectLoyaltyEarned, recordEarned(repo, log)},
		{"user deleted events", data.SubjectUserDeleted, accountEvents(accountChange{"deletion", repo.DeleteAccount}, log)},
		{"user lifecycle events", data.SubjectUserChanged, accountEvents(accountChange{"lifecycle", repo.RestoreUser}, log)},
	}
	for _, s := range subs {
		if err := bus.Consume(ctx, nrApp, grouped, s.subject, s.handle, log); err != nil {
			return nil, fmt.Errorf("subscribe to %s: %w", s.name, err)
		}
	}
	return bus.ConsumeConcurrent(ctx, nrApp, grouped, data.SubjectLoyaltyCounters, counterWorkers, recordBumps(counters, log), log)
}

type earnedProcessor interface {
	ApplyEarned(context.Context, data.LoyaltyEarnedDTO) error
}

func recordEarned(repo earnedProcessor, log *zap.Logger) func(*bus.Message) error {
	return loyaltyBatchHandler("earned", log, func(ctx context.Context, dto data.LoyaltyEarnedDTO, messageID string) error {
		if dto.UserID == 0 || len(dto.Entries) == 0 {
			return nil
		}
		dto.BatchID = batchIdentity(dto.BatchID, messageID)
		return repo.ApplyEarned(ctx, dto)
	})
}

type counterProcessor interface {
	Process(context.Context, data.CounterBumpedDTO) error
}

func recordBumps(repo counterProcessor, log *zap.Logger) func(*bus.Message) error {
	return loyaltyBatchHandler("counter", log, func(ctx context.Context, dto data.CounterBumpedDTO, messageID string) error {
		dto.BatchID = batchIdentity(dto.BatchID, messageID)
		return repo.Process(ctx, dto)
	})
}

func batchIdentity(batchID, messageID string) string {
	if batchID != "" {
		return batchID
	}
	return messageID
}

// All additive batches share decode and invalid-input policy. The callback
// supplies domain validation and persistence; transient failures reach the bus.
func loyaltyBatchHandler[T any](kind string, log *zap.Logger, apply func(context.Context, T, string) error) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)
		var dto T
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad "+kind+" payload", zap.Error(err))
			return nil
		}
		err := apply(msg.Context(), dto, msg.UUID)
		if errors.Is(err, repository.ErrInvalidInput) {
			log.Warn("loyalty: dropping invalid "+kind+" batch", zap.String("message_id", msg.UUID), zap.Error(err))
			return nil
		}
		return err
	}
}

type accountChange struct {
	payload string
	apply   func(ctx context.Context, userID uint64, accountCreatedAt int64) error
}

// UserChangedDTO carries UserDeletedDTO's identity fields under the same JSON names.
func accountEvents(change accountChange, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserChangedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("loyalty: bad "+change.payload+" payload", zap.Error(err))
			return nil
		}
		return change.apply(msg.Context(), dto.UserID, dto.AccountCreatedAt)
	}
}

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log
	shutdown := shutdownContext(core.Ctx, shutdownBudget)

	driver := databoot.MustEntDriver(core, "bagel_loyalty")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error {
		return repository.MigrateSchema(ctx, client, driver.Dialect())
	})

	repo := repository.NewLoyalty(client, driver, core.NR, log)
	defer repo.Close(context.Background())
	svcboot.FatalIf(log, repo.EnsureWatchSchema(core.Ctx), "failed to initialize watch inbox")
	vc := svcboot.MustValkey(core)
	defer vc.Close()
	counters := repository.NewCounterProcessor(repo, vc)
	watchCtx, stopWatch := context.WithCancel(core.Ctx)
	watchDone := make(chan struct{})
	watchConsumer := watchtime.NewConsumer(vc, repo.ApplyWatchAward, log, watchtime.WithHistoryMaintenance(repo.PruneWatchHistory))
	svcboot.FatalIf(log, watchConsumer.Ensure(core.Ctx), "failed to initialize watch outbox consumer")
	go func() { defer close(watchDone); watchConsumer.Run(watchCtx) }()
	defer func() { stopWatch(); <-watchDone }()

	nc := svcboot.MustRPCConn(core, bus.RPCURL(core.NATSURL))
	defer nc.Close()
	invalidationPrefix := conf.CacheInvalidationPrefix.Get()
	repo.SetInvalidation(nc, invalidationPrefix)

	grouped, err := bus.NewSubscriber(core.NATSURL, serviceName, log)
	svcboot.FatalIf(log, err, "failed to connect group subscriber")
	var counterConsumer *bus.ConcurrentConsumer
	defer func() { counterIntake{grouped, counterConsumer, counters}.stop(shutdown, log) }()

	counterConsumer, err = registerConsumers(core.Ctx, core.NR, repo, counters, grouped, log)
	svcboot.FatalIf(log, err, "failed to subscribe to events")

	loyaltyPrefix := conf.LoyaltyPrefix.Get()
	svcboot.FatalIf(log, rpc.Subscribe(rpc.Wiring{
		RPCWiring:          bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log},
		Repo:               repo,
		InvalidationPrefix: invalidationPrefix,
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
