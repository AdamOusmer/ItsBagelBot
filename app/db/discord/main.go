// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/discord/ent"
	// Without the ent runtime import every write fails.
	_ "ItsBagelBot/app/db/discord/ent/runtime"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/app/db/discord/rpc"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
	"ItsBagelBot/pkg/svcboot/databoot"

	"go.uber.org/zap"
)

const (
	serviceName    = "discord-data"
	queueGroup     = "discord-data-rpc"
	defaultSchema  = "bagel_discord"
	defaultNATSURL = "nats://127.0.0.1:4222"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	driver := databoot.MustEntDriver(core, defaultSchema)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	databoot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	nc := svcboot.MustRPCConn(core, rpcEndpoint())
	defer nc.Close()

	repo := repository.New(client, driver.Dialect())
	grouped, err := bus.NewSubscriber(core.NATSURL, serviceName, log)
	svcboot.FatalIf(log, err, "failed to connect group subscriber")
	defer func() { _ = grouped.Close() }()
	svcboot.FatalIf(log, bus.Consume(core.Ctx, core.NR, grouped, data.SubjectUserDeleted, eraseDeletedAccount(repo, log), log),
		"failed to subscribe to user deleted events")

	prefix := env.Get("NATS_DISCORD_DATA_SUBJECT_PREFIX", "bagel.rpc.discord-data")
	err = rpc.Subscribe(rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, App: core.NR, Queue: queueGroup, Log: log},
		Repo:      repo,
		Prefix:    prefix,
	})
	if err != nil {
		log.Fatal("failed to subscribe discord-data rpc", zap.Error(err))
	}

	databoot.ServeHealth(databoot.Health{
		Health: svcboot.Health{
			Log: log, NC: nc, Service: serviceName, QueueGroup: queueGroup, ListenAddr: core.ListenAddr,
		},
		Pool: driver.DB(),
	}, bus.LaneCheck("data", grouped))

	log.Info("discord-data service ready", zap.String("prefix", prefix))

	core.Await()
}

// Must not become svcboot.MustNATS: discord_data_bus only consumes data.users.deleted.
func rpcEndpoint() string {
	return bus.RPCURL(env.Get("NATS_URL", defaultNATSURL))
}

func eraseDeletedAccount(repo *repository.Store, log *zap.Logger) func(*bus.Message) error {
	return func(msg *bus.Message) error {
		var dto data.UserDeletedDTO
		if err := codec.Unmarshal(msg.Payload, &dto); err != nil {
			log.Warn("discord-data: bad user_deleted payload", zap.Error(err))
			return nil
		}
		deletedAt := dto.DeletedAt
		if deletedAt.IsZero() {
			deletedAt = msg.StoredAt()
		}
		_, err := repo.EraseBroadcaster(msg.Context(), dto.UserID, deletedAt)
		if errors.Is(err, repository.ErrInvalidInput) {
			log.Warn("discord-data: unusable user_deleted event", zap.Uint64("user_id", dto.UserID))
			return nil
		}
		return err
	}
}
