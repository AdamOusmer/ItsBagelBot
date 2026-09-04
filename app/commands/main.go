// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"

	"github.com/newrelic/go-agent/v3/newrelic"

	"ItsBagelBot/app/commands/ent"
	// Wire the ent schema runtime (field defaults like updated_at, and the name
	// normalization hook). Without this blank import the generated descriptors
	// stay uninitialized and every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/commands/ent/runtime"
	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/app/commands/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/svcboot"

	"go.uber.org/zap"
)

const serviceName = "commands"

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
		{"command changes", broadcast, data.SubjectCommandChanged, invalidateOnChange(repo)},
		{"fetch changes", broadcast, data.SubjectFetchChanged, invalidateFetchOnChange(fetches)},
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

// invalidateOnChange drops the cached view of the changed user.
func invalidateOnChange(repo *repository.Commands) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.CommandChangedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}
		repo.Invalidate(dto.UserID)
		return nil
	}
}

// invalidateFetchOnChange drops the cached fetch view of the changed user.
func invalidateFetchOnChange(repo *repository.Fetches) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.FetchChangedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			return err
		}
		repo.Invalidate(dto.UserID)
		return nil
	}
}

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
// a deleted account. Malformed or invalid payloads are dropped; a DB failure
// is returned for retry.
func deleteAllForUser(repo *repository.Commands, fetches *repository.Fetches, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("commands: bad user_deleted payload", zap.Error(err))
			return nil
		}
		if err := validate.UserID(dto.UserID); err != nil {
			log.Warn("commands: invalid user_id in user_deleted", zap.Error(err))
			return nil
		}
		if err := repo.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
			return err
		}
		if err := fetches.DeleteAllForUser(msg.Context(), dto.UserID); err != nil {
			return err
		}
		log.Info("commands: deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := svcboot.MustEntDriver(log, "bagel_commands")
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	svcboot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core, serviceName, "commands-rpc")
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

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")
	if err := rpc.SubscribeProjection(n.RPC, repo, projectionSubject, "commands-rpc", core.NR, log); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	fetchesProjectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_FETCHES_SUBJECT", "bagel.rpc.internal.projection.commands.fetches.get")
	if err := rpc.SubscribeFetchProjection(n.RPC, fetches, fetchesProjectionSubject, "commands-rpc", core.NR, log); err != nil {
		log.Fatal("failed to subscribe fetches projection rpc", zap.Error(err))
	}

	commandsPrefix := env.Get("NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands")
	if err := rpc.SubscribeDashboard(n.RPC, repo, commandsPrefix, "commands-rpc", core.NR, log); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}
	if err := rpc.SubscribeFetchDashboard(rpc.FetchDashboardWiring{
		NC: n.RPC, Repo: fetches, Prefix: commandsPrefix, QueueGroup: "commands-rpc", App: core.NR, Log: log,
	}); err != nil {
		log.Fatal("failed to subscribe fetch dashboard rpc", zap.Error(err))
	}

	fetchKeySubject := env.Get("NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.commands.fetchkey") + ".get"
	if err := rpc.SubscribeFetchKey(rpc.FetchKeySubscription{
		NC:         n.RPC,
		Repo:       fetches,
		Subject:    fetchKeySubject,
		QueueGroup: "commands-rpc",
		App:        core.NR,
		Log:        log,
	}); err != nil {
		log.Fatal("failed to subscribe fetch key rpc", zap.Error(err))
	}

	// The lane check covers the durable group folding data.commands.used and
	// data.users.deleted: a consumer that stays bound while failing to fetch
	// stops the use counters silently, with NATS and MySQL both still reading
	// green. The broadcast subscriber is not checked -- it has no fetch loop to
	// wedge.
	svcboot.ServeDataHealth(svcboot.DataHealth{
		Log: log, NC: n.RPC, Service: serviceName, QueueGroup: "commands-rpc", Pool: driver.DB(),
	}, bus.LaneCheck("data", n.Grouped))

	log.Info("commands service ready",
		zap.String("projection_subject", projectionSubject),
		zap.String("fetches_projection_subject", fetchesProjectionSubject),
		zap.String("commands_prefix", commandsPrefix),
		zap.String("fetch_key_subject", fetchKeySubject),
		zap.Bool("key_custody", fetches.CustodyEnabled()),
	)

	<-core.Ctx.Done()

	log.Info("commands service shutting down")
}
