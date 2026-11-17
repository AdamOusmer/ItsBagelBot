// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/internal/analytics"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/db/commands/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/commands/ent/runtime"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/app/db/commands/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/consumers"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/env/conf"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName      = "commands"
	queueGroup       = "commands-rpc"
	terminationGrace = 45 * time.Second
	preStopDrain     = health.DrainDelay
	exitReserve      = 5 * time.Second
	shutdownBudget   = terminationGrace - preStopDrain - exitReserve
)

type consumerWiring struct {
	app     *newrelic.Application
	repo    *repository.Commands
	fetches *repository.Fetches
	grouped bus.Subscriber
	log     *zap.Logger
}

func registerConsumers(ctx context.Context, w consumerWiring) (*bus.ConcurrentConsumer, error) {
	subs := []struct {
		name    string
		sub     bus.Subscriber
		subject string
		handle  func(*bus.Message) error
	}{
		{"user deleted events", w.grouped, data.SubjectUserDeleted, deleteAllForUser(w.repo, w.fetches, w.log)},
	}
	for _, s := range subs {
		if err := bus.Consume(ctx, w.app, s.sub, s.subject, s.handle, w.log); err != nil {
			return nil, fmt.Errorf("subscribe to %s: %w", s.name, err)
		}
	}
	return bus.ConsumeDemand(ctx, bus.DemandConsumerConfig{
		App: w.app, Subscriber: w.grouped, Subject: data.SubjectCommandUsed,
		Handle: recordUse(w.repo, w.log), Log: w.log,
	})
}

type commandShutdown struct {
	consumer    *bus.ConcurrentConsumer
	closeIntake func()
	repo        *repository.Commands
	log         *zap.Logger
	started     <-chan time.Time
}

func (s commandShutdown) stop() {
	started := time.Now()
	select {
	case started = <-s.started:
	default:
	}
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(shutdownBudget))
	defer cancel()
	intakeDone := make(chan struct{})
	go func() { s.closeIntake(); close(intakeDone) }()
	select {
	case <-intakeDone:
	case <-ctx.Done():
		s.log.Fatal("command intake shutdown budget exhausted", zap.Error(ctx.Err()))
	}
	if err := s.consumer.Drain(ctx); err != nil {
		s.log.Error("command use consumer drain failed", zap.Error(err))
	}
	if err := s.repo.Close(ctx); err != nil {
		s.log.Fatal("commands shutdown budget exhausted with workers still active", zap.Error(err))
	}
}

func recordUse(repo *repository.Commands, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.CommandUsedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("commands: bad command_used payload", zap.Error(err))
			return nil
		}
		batchID := dto.BatchID
		if batchID == "" {
			batchID = msg.UUID
		}
		return repo.RecordUse(msg.Context(), batchID, dto)
	}
}

func deleteAllForUser(repo *repository.Commands, fetches *repository.Fetches, log *zap.Logger) func(*bus.Message) error {
	return consumers.OnUserDeleted(serviceName, log, func(ctx context.Context, userID uint64, cutoff time.Time) error {
		if err := repo.DeleteAllForUser(ctx, userID, cutoff); err != nil {
			return err
		}
		return fetches.DeleteAllForUser(ctx, userID, cutoff)
	})
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log
	shutdownStarted := make(chan time.Time, 1)
	context.AfterFunc(core.Ctx, func() { shutdownStarted <- time.Now() })

	driver := databoot.MustEntDriver(core)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()

	recorder := analytics.New(svcboot.MustValkey(core), log, nil)
	defer recorder.Close()

	h := bus.NewInvalidations(n.RPC, conf.CacheInvalidationPrefix.Get(), log)
	pub := h.Publisher(n.Pub)
	repo := repository.NewCommands(client, pub, core.NR, log)
	repo.SetAnalytics(recorder)

	// Must run before RPC and consumer traffic so a concurrent edit cannot race it.
	if err := repo.BackfillBumpCounterFromTokens(core.Ctx); err != nil {
		log.Error("bump_counter backfill failed; commands keep their prior (unset) bump_counter", zap.Error(err))
	}

	fetches := repository.NewFetches(client, repository.NewFetchesFromEnv(log), pub, log)
	levels := analytics.NewLevelSource(analytics.Owner{
		Service: serviceName,
		Owned:   analytics.Owned{"custom.total", "custom.perm", "custom.flags", "custom.channels", "integrations.connected|fetch_key"},
		Seed:    repository.SeedAll(repo, fetches),
	})
	defer fetches.Close()

	svcboot.FatalIf(log, h.Changes(core.Ctx, core.NR, n.Broadcast, data.SubjectCommandChanged, data.SubjectFetchChanged), "failed to subscribe changes")
	consumer, err := registerConsumers(core.Ctx, consumerWiring{core.NR, repo, fetches, n.Grouped, log})
	if err != nil {
		log.Fatal("failed to subscribe to events", zap.Error(err))
	}
	defer (commandShutdown{consumer, closeIntake, repo, log, shutdownStarted}).stop()

	svcboot.FatalIf(log, levels.Serve(bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log}), "failed to subscribe analytics levels rpc")

	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log},
		Commands:  repo,
		Fetches:   fetches,
	}

	projectionSubject := conf.ProjectionCommandsSubject.Get()
	if err := rpc.SubscribeProjection(wiring, projectionSubject); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	fetchesProjectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_FETCHES_SUBJECT", "rpc.internal.projection.commands.fetches.get")
	if err := rpc.SubscribeFetchProjection(wiring, fetchesProjectionSubject); err != nil {
		log.Fatal("failed to subscribe fetches projection rpc", zap.Error(err))
	}

	commandsPrefix := conf.CommandsPrefix.Get()
	if err := rpc.SubscribeDashboard(wiring, commandsPrefix); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}
	if err := rpc.SubscribeFetchDashboard(wiring, commandsPrefix); err != nil {
		log.Fatal("failed to subscribe fetch dashboard rpc", zap.Error(err))
	}

	fetchKeySubject := conf.FetchKeyPrefix.Get() + ".get"
	if err := rpc.SubscribeFetchKey(wiring, fetchKeySubject); err != nil {
		log.Fatal("failed to subscribe fetch key rpc", zap.Error(err))
	}

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", n.Grouped))

	log.Info("commands service ready",
		zap.String("projection_subject", projectionSubject),
		zap.String("fetches_projection_subject", fetchesProjectionSubject),
		zap.String("commands_prefix", commandsPrefix),
		zap.String("fetch_key_subject", fetchKeySubject),
		zap.Bool("key_custody", fetches.CustodyEnabled()),
	)

	core.Await()
}
