// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/db/commands/ent"
	// Wire the ent schema runtime (field defaults like updated_at, and the name
	// normalization hook). Without this blank import the generated descriptors
	// stay uninitialized and every write fails: "forgotten import ent/runtime?".
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

// registerConsumers wires the event subscriptions onto repo: cache
// invalidation fans out to every instance (broadcast), while use-counter and
// account-deletion events are handled once per event (grouped).
func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Commands, fetches *repository.Fetches, broadcast, grouped bus.Subscriber, log *zap.Logger) error {
	// Use-counter events from the worker: exactly one instance sums each event
	// (queue group), the repo batches them and flushes uses = uses + n.
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

// changedUserID and fetchChangedUserID read the account off a change event.
// Go cannot reach a field through a type parameter, so the shared
// invalidation consumer takes these instead of one reflective accessor.
func changedUserID(dto data.CommandChangedDTO) uint64 { return dto.UserID }

func fetchChangedUserID(dto data.FetchChangedDTO) uint64 { return dto.UserID }

// recordUse folds a worker use-counter event into the repo's accumulator. A
// malformed payload is dropped (nil), not retried.
func recordUse(repo *repository.Commands, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.CommandUsedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("commands: bad command_used payload", zap.Error(err))
			return nil
		}
		repo.RecordUse(dto.UserID, dto.Name, dto.Count)
		return nil
	}
}

// deleteAllForUser removes every command, fetch definition and sealed key of
// a deleted account. The payload guards and the log line are shared with the
// other data services; only the two sweeps are this service's own.
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
	defer repo.Close(context.Background()) // flushes pending writes on shutdown
	defer closeIntake()                    // stops intake before the repo flush above

	// Best-effort keyset load (modules-style): an unset path or an absent
	// optional mount warns and disables key custody — definitions keep
	// working keyless — while a present-but-invalid keyset is fatal inside
	// NewFetchesFromEnv. commands rides the core chat path even with zero
	// keys ever sealed, so it must not crash-loop on a secret that may not be
	// provisioned yet.
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

	// The lane check covers the durable group folding data.commands.used and
	// data.users.deleted: a consumer that stays bound while failing to fetch
	// stops the use counters silently, with NATS and MySQL both still reading
	// green. The broadcast subscriber is not checked -- it has no fetch loop to
	// wedge.
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
