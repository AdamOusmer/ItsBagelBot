// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"

	"ItsBagelBot/app/db/discord/ent"
	// Wire the ent schema runtime (field defaults/hooks); without this blank
	// import every write fails: "forgotten import ent/runtime?".
	_ "ItsBagelBot/app/db/discord/ent/runtime"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/app/db/discord/rpc"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"

	"go.uber.org/zap"
)

const (
	serviceName = "discord-data"
	queueGroup  = "discord-data-rpc"
	// defaultSchema is the per-service MySQL schema (ADR 0007: one schema per
	// data service, no cross-schema foreign keys).
	defaultSchema = "bagel_discord"
)

func main() {
	core, done := svcboot.NewCore(serviceName)
	defer done()
	log := core.Log

	// Boot order is load-bearing: driver -> client -> migrate -> NATS -> repo
	// -> rpc -> health. The health Set reports on the pool, so it cannot be
	// built before the pool exists, and the RPC surface must not answer before
	// the schema it queries is there.
	driver := svcboot.MustEntDriver(log, defaultSchema)
	client := ent.NewClient(ent.Driver(driver))
	defer func() { _ = client.Close() }()

	svcboot.AutoMigrate(core.Ctx, log, func(ctx context.Context) error { return client.Schema.Create(ctx) })

	n, closeIntake := svcboot.MustNATS(core, serviceName, queueGroup)
	defer func() { _ = n.Pub.Close() }()
	defer closeIntake()

	prefix := env.Get("NATS_DISCORD_DATA_SUBJECT_PREFIX", "bagel.rpc.discord-data")
	err := rpc.Subscribe(rpc.Wiring{
		NC:         n.RPC,
		Repo:       repository.New(client, driver.Dialect()),
		Prefix:     prefix,
		QueueGroup: queueGroup,
		App:        core.NR,
		Log:        log,
	})
	if err != nil {
		log.Fatal("failed to subscribe discord-data rpc", zap.Error(err))
	}

	// No lane check: this service consumes no event lane, only request/reply.
	svcboot.ServeDataHealth(svcboot.DataHealth{
		Log: log, NC: n.RPC, Service: serviceName, QueueGroup: queueGroup, Pool: driver.DB(),
	})

	log.Info("discord-data service ready", zap.String("prefix", prefix))

	<-core.Ctx.Done()

	log.Info("discord-data service shutting down")
}
