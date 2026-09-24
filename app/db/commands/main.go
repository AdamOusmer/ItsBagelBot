// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"

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
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName = "commands"
	queueGroup  = "commands-rpc"
)

func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Commands, fetches *repository.Fetches, broadcast, grouped bus.Subscriber, log *zap.Logger) error {
	subs := []struct {
		name    string
		sub     bus.Subscriber
		subject string
		handle  func(*bus.Message) error
	}{
		{"command changes", broadcast, data.SubjectCommandChanged, consumers.OnChangeInvalidate(changedUserID, repo.Invalidate)},
		{"fetch changes", broadcast, data.SubjectFetchChanged, consumers.OnChangeInvalidate(fetchChangedUserID, fetches.Invalidate)},
		{"command used events", grouped, data.SubjectCommandUsed, recordUse(repo, log)},
		{"user deleted events", grouped, data.SubjectUserDeleted, deleteAllForUser(repo, fetches, log)},
	}
	for _, s := range subs {
		if err := bus.Consume(ctx, nrApp, s.sub, s.subject, s.handle, log); err != nil {
			return fmt.Errorf("subscribe to %s: %w", s.name, err)
		}
	}
	return nil
}

func changedUserID(dto data.CommandChangedDTO) uint64 { return dto.UserID }

func fetchChangedUserID(dto data.FetchChangedDTO) uint64 { return dto.UserID }

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
		return repo.RecordUse(msg.Context(), batchID, dto.UserID, dto.Name, dto.Count)
	}
}

func deleteAllForUser(repo *repository.Commands, fetches *repository.Fetches, log *zap.Logger) func(*bus.Message) error {
	return consumers.OnUserDeleted(serviceName, log, func(ctx context.Context, userID uint64) error {
		if err := repo.DeleteAllForUser(ctx, userID); err != nil {
			return err
		}
		return fetches.DeleteAllForUser(ctx, userID)
	})
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, "bagel_commands")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core)
	defer func() { _ = n.Pub.Close() }()

	repo := repository.NewCommands(client, n.Pub, core.NR, log)
	defer repo.Close(context.Background())
	defer closeIntake() // stops intake before the repo flush above

	// Must run before RPC and consumer traffic so a concurrent edit cannot race it.
	if err := repo.BackfillBumpCounterFromTokens(core.Ctx); err != nil {
		log.Error("bump_counter backfill failed; commands keep their prior (unset) bump_counter", zap.Error(err))
	}

	fetches := repository.NewFetches(client, repository.NewFetchesFromEnv(log), n.Pub, log)
	defer fetches.Close()

	if err := registerConsumers(core.Ctx, core.NR, repo, fetches, n.Broadcast, n.Grouped, log); err != nil {
		log.Fatal("failed to subscribe to events", zap.Error(err))
	}

	wiring := rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: n.RPC, App: core.NR, Queue: queueGroup, Log: log},
		Commands:  repo,
		Fetches:   fetches,
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")
	if err := rpc.SubscribeProjection(wiring, projectionSubject); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	fetchesProjectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_FETCHES_SUBJECT", "bagel.rpc.internal.projection.commands.fetches.get")
	if err := rpc.SubscribeFetchProjection(wiring, fetchesProjectionSubject); err != nil {
		log.Fatal("failed to subscribe fetches projection rpc", zap.Error(err))
	}

	commandsPrefix := env.Get("NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands")
	if err := rpc.SubscribeDashboard(wiring, commandsPrefix); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}
	if err := rpc.SubscribeFetchDashboard(wiring, commandsPrefix); err != nil {
		log.Fatal("failed to subscribe fetch dashboard rpc", zap.Error(err))
	}

	fetchKeySubject := env.Get("NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.commands.fetchkey") + ".get"
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
