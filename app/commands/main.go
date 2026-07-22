package main

import (
	"context"
	"encoding/json"
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
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/health"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/svcboot"

	"go.uber.org/zap"
)

const serviceName = "commands"

// registerConsumers wires the event subscriptions onto repo: cache
// invalidation fans out to every instance (broadcast), while use-counter and
// account-deletion events are handled once per event (grouped).
func registerConsumers(ctx context.Context, nrApp *newrelic.Application, repo *repository.Commands, broadcast, grouped bus.Subscriber, log *zap.Logger) error {
	// Use-counter events from the worker: exactly one instance sums each event
	// (queue group), the repo batches them and flushes uses = uses + n.
	subs := []struct {
		name    string
		sub     bus.Subscriber
		subject string
		handle  func(*bus.Message) error
	}{
		{"command changes", broadcast, data.SubjectCommandChanged, invalidateOnChange(repo)},
		{"command used events", grouped, data.SubjectCommandUsed, recordUse(repo, log)},
		{"user deleted events", grouped, data.SubjectUserDeleted, deleteAllForUser(repo, log)},
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
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
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
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("commands: bad command_used payload", zap.Error(err))
			return nil
		}
		repo.RecordUse(dto.UserID, dto.Name, dto.Count)
		return nil
	}
}

// deleteAllForUser removes every command of a deleted account. Malformed or
// invalid payloads are dropped; a DB failure is returned for retry.
func deleteAllForUser(repo *repository.Commands, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		log := monitor.TxnLogger(msg.Context(), log)
		var dto data.UserDeletedDTO
		if err := json.Unmarshal(msg.Payload, &dto); err != nil {
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
		log.Info("commands: deleted all for user", zap.Uint64("user_id", dto.UserID))
		return nil
	}
}

func main() {
	validate.CheckFloor = moderation.CheckFloor

	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	client := ent.NewClient(ent.Driver(svcboot.MustEntDriver(log, "bagel_commands")))
	defer func() { _ = client.Close() }()

	svcboot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core, serviceName, "commands-rpc")
	defer func() { _ = n.Pub.Close() }()

	repo := repository.NewCommands(client, n.Pub, core.NR, log)
	defer repo.Close(context.Background()) // flushes pending writes on shutdown
	defer closeIntake()                    // stops intake before the repo flush above

	if err := registerConsumers(core.Ctx, core.NR, repo, n.Broadcast, n.Grouped, log); err != nil {
		log.Fatal("failed to subscribe to events", zap.Error(err))
	}

	projectionSubject := env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get")
	if err := rpc.SubscribeProjection(n.RPC, repo, projectionSubject, "commands-rpc", core.NR, log); err != nil {
		log.Fatal("failed to subscribe projection rpc", zap.Error(err))
	}

	commandsPrefix := env.Get("NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands")
	if err := rpc.SubscribeDashboard(n.RPC, repo, commandsPrefix, "commands-rpc", core.NR, log); err != nil {
		log.Fatal("failed to subscribe dashboard rpc", zap.Error(err))
	}

	health.Serve(env.Get("LISTEN_ADDR", ":8080"), n.RPC.IsConnected)

	log.Info("commands service ready",
		zap.String("projection_subject", projectionSubject),
		zap.String("commands_prefix", commandsPrefix),
	)

	<-core.Ctx.Done()

	log.Info("commands service shutting down")
}
